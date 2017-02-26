package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
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
	var (
		list       bool
		configPath string
	)

	flag.BoolVar(&update, "u", false, "Update all resources.")
	flag.BoolVar(&list, "l", false, "List configured courses.")
	flag.StringVar(&configPath, "c", "~/.frifetch.json", "Configuration file location")
	flag.Parse()

	conf := initConf(findConf(configPath), flag.Args())

	if list {
		for k, v := range conf.Courses {
			log.Printf("%4s %d\n", k, v)
		}
		return
	}

	c := login(conf)
	for k, v := range conf.Courses {
		url := fmt.Sprintf("%s/course/resources.php?id=%d", conf.RootURL, v)
		p := path.Join(conf.Path, k)
		log.Println("Course", k, v)
		if !fileExists(p) {
			if err := os.MkdirAll(p, 0700); err != nil {
				log.Fatal(err)
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

func findConf(configPath string) string {
	if strings.Contains(configPath, "~") {
		u, err := user.Current()
		if err != nil {
			log.Fatal(err)
		}
		return strings.Replace(configPath, "~", u.HomeDir, 1)
	}
	return configPath
}

func initConf(config string, course []string) Conf {
	var c Conf

	f, err := os.Open(config)
	if err != nil {
		log.Fatal(err)
	}

	d := json.NewDecoder(f)
	err = d.Decode(&c)
	if err != nil {
		log.Fatal(err)
	}

	if c.PasswordCmd != "" {
		inst := strings.Split(c.PasswordCmd, " ")
		cmd := exec.Command(inst[0], inst[1:]...)

		var out bytes.Buffer
		cmd.Stdout = &out

		err := cmd.Run()
		if err != nil {
			log.Fatal(err)
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

	return c
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
		log.Fatal(err)
	}
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
			fetch(c, v, p)
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

func fetch(c *http.Client, url, fileName string) {
	if !update && fileExists(fileName) {
		return
	}
	log.Println(fileName)

	res, err := c.Get(url)
	if err != nil {
		log.Println(err)
		return
	}

	f, err := os.Create(fileName)
	if err != nil {
		log.Println(err)
		return
	}

	_, err = io.Copy(f, res.Body)
	if err != nil {
		log.Println(err)
		return
	}

	err = f.Close()
	if err != nil {
		log.Println(err)
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
		if idPrefix != "" {
			return idPrefix
		}
		return "empty"
	}
	_, params, _ := mime.ParseMediaType(disp)
	if name, ok := params["filename"]; ok {
		return name
	}
	return "empty"
}

func validName(name string, recurse bool) bool {
	examples := []struct {
		matcher string
		recurse bool
	}{
		{"/resource/view.php", false},
		{"/folder/view.php", true},
		{"/resource/view.php", false},
		{"mod_label/intro/", false},
		{"mod_page/content", false},
	}

	for _, v := range examples {
		if v.recurse {
			if recurse && strings.Contains(name, v.matcher) {
				return true
			}
			continue
		}

		if strings.Contains(name, v.matcher) {
			return true
		}
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

// https://ucilnica.fri.uni-lj.si/theme/image.php/clean/core/1482534895/f/pdf-24
