package main

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"strings"

	"encoding/json"

	"os/exec"

	"golang.org/x/net/html"
)

func main() {
	conf, err := initConf()
	if err != nil {
		fmt.Println(err)
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
		crawl(c, url, p)
	}
}

// Conf is the marshalling struct
type Conf struct {
	Username, Path, RootURL string
	Password, PasswordCmd   string
	Courses                 map[string]int
}

func initConf() (Conf, error) {
	var c Conf

	f, err := os.Open("conf.json")
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

func crawl(c *http.Client, u, filePath string) {
	res, _ := c.Get(u)
	urls := links(res.Body)
	for _, v := range urls {
		res, err := c.Head(v)
		if err != nil {
			fmt.Println(err)
			return
		}

		fileName := parseName(res.Header.Get("Content-Disposition"))
		p := path.Join(filePath, fileName)
		dwn(c, v, p)
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
	if fileExists(fileName) {
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

func parseName(disp string) string {
	if disp == "" {
		return "empty"
	}
	_, params, _ := mime.ParseMediaType(disp)
	if name, ok := params["filename"]; ok {
		return name
	}
	return "empty"
}

func links(r io.Reader) []string {
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
					if a.Key == "href" && strings.Contains(a.Val, "/resource/view.php") {
						urls = append(urls, a.Val)
					}
				}
			}
		}
	}
}
