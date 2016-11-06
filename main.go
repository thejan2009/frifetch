package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"

	"encoding/json"

	"net/http"
	"net/http/cookiejar"
	"net/url"

	"golang.org/x/net/html"

	"os"
	"os/exec"
	"os/user"
)

var update bool

func main() {
	flag.BoolVar(&update, "u", false, "Update all resources.")
	list := flag.Bool("l", false, "List configured courses.")
	config := flag.String("c", "~/.frifetch.json", "Configuration file location")
	flag.Parse()

	conf, err := initConf(findConf(config), flag.Args())
	if err != nil {
		fmt.Println(err)
		return
	}

	if *list {
		for k, v := range conf.Courses {
			fmt.Printf("%4s %d\n", k, v)
		}
		return
	}

	c := login(conf)
	for k, v := range conf.Courses {
		url := fmt.Sprintf("%s/course/view.php?id=%d", conf.RootURL, v)
		p := path.Join(conf.Path, k)
		fmt.Println("Course", k, v)
		if !fileExists(p) {
			if err := os.MkdirAll(p, 0700); err != nil {
				fmt.Println(err)
				return
			}
		}
		crawl(c, true, url, p)
	}
}

// Conf is the marshalling struct
type Conf struct {
	Username, Path, RootURL string
	Password, PasswordCmd   string
	Courses                 map[string]int
}

func findConf(config *string) string {
	if strings.Contains(*config, "~") {
		u, err := user.Current()
		if err != nil {
			fmt.Println(err)
			return ""
		}
		return strings.Replace(*config, "~", u.HomeDir, 1)
	}
	return *config
}

func initConf(config string, course []string) (Conf, error) {
	var c Conf

	f, err := os.Open(config)
	if err != nil {
		fmt.Println(err)
		return c, err
	}
	d := json.NewDecoder(f)

	err = d.Decode(&c)
	if err != nil {
		fmt.Println(err)
		return c, err
	}

	if c.PasswordCmd != "" {
		inst := strings.Split(c.PasswordCmd, " ")
		cmd := exec.Command(inst[0], inst[1:]...)

		var out bytes.Buffer
		cmd.Stdout = &out

		err := cmd.Run()
		if err != nil {
			return c, err
		}

		c.Password = out.String()
	}

	if len(course) != 0 {
		newCourses := make(map[string]int)
		for _, v := range course {
			if el, ok := c.Courses[v]; ok {
				newCourses[v] = el
			}
		}
		c.Courses = newCourses

	}
	return c, nil
}

func login(c Conf) *http.Client {
	loginURL := c.RootURL + "/login/index.php"
	jar, _ := cookiejar.New(nil)
	client := http.Client{
		Jar: jar,
	}

	f := url.Values{}
	f.Add("username", c.Username)
	f.Add("password", c.Password)

	_, err := client.PostForm(loginURL, f)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("Login successful.")
	return &client
}

func crawl(c *http.Client, recurse bool, u, filePath string) {
	res, _ := c.Get(u)
	urls := links(res.Body, recurse)
	for _, v := range urls {
		if strings.Contains(v, "/folder/view.php") {
			crawl(c, false, v, filePath)
		} else {
			res, err := c.Head(v)
			if err != nil {
				fmt.Println(err)
				return
			}

			fileName := parseName(res.Header.Get("Content-Disposition"), v)
			p := path.Join(filePath, fileName)
			dwn(c, v, p)
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return false
}

func dwn(c *http.Client, url, fileName string) {
	if !update && fileExists(fileName) {
		fmt.Println(fileName, "exists")
		return
	}
	fmt.Println(fileName)

	res, err := c.Get(url)
	if err != nil {
		fmt.Println(err)
		return
	}

	f, err := os.Create(fileName)
	if err != nil {
		fmt.Println(err)
		return
	}

	_, err = io.Copy(f, res.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	err = f.Close()
	if err != nil {
		fmt.Println(err)
		return
	}
}

func parseName(disp string, url string) string {
	idPrefix := ""
	parts := strings.Split(url, "?id=")
	if len(parts) == 2 {
		idPrefix = parts[1]
	}

	if disp == "" {
		return "empty"
	}
	_, params, _ := mime.ParseMediaType(disp)
	if name, ok := params["filename"]; ok {
		return idPrefix + name
	}
	return "empty"
}

func validName(name string, recurse bool) bool {
	if strings.Contains(name, "/resource/view.php") {
		return true
	}
	if recurse && strings.Contains(name, "/folder/view.php") {
		return true
	}
	if strings.Contains(name, "mod_folder/content/") {
		return true
	}
	if strings.Contains(name, "mod_label/intro/") {
		return true
	}

	return false
}

func links(r io.Reader, recurse bool) []string {
	var urls []string
	z := html.NewTokenizer(r)

	for {
		tok := z.Next()

		switch {
		case tok == html.ErrorToken:
			return urls
		case tok == html.StartTagToken:
			t := z.Token()

			if t.Data == "a" {
				for _, a := range t.Attr {
					// TODO: figure out some other legal urls
					if a.Key == "href" && validName(a.Val, recurse) {
						urls = append(urls, a.Val)
					}
				}
			}
		}
	}
}
