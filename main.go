package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/asdine/storm"
	"github.com/asdine/storm/q"
	"github.com/gin-gonic/gin"
)

type Queries []Query
type Query struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Src         string `json:"src,omitempty"`
}

type CreativesSlice struct {
	Creatives []Creative `json:"creatives_to_follow"`
}

type Creative struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

type UserProjectsSlice struct {
	Projects []UserProject `json:"projects"`
}

type UserProject struct {
	ID int `json:"id"`
}

type Project struct {
	Project ProjectParsed `json:"project"`
}

type ProjectParsed struct {
	Title       string                 `json:"name"`
	Description string                 `json:"description"`
	Src         map[string]interface{} `json:"covers"`
}

type Data struct {
	ID          int    `storm:"id,increment" json:"id"`
	Title       string `storm:"index" json:"title"`
	Description string `json:"description"`
	Filename    string `storm:"index" json:"filename"`
	Src         string `storm:"index" json:"src"`
}

type Env struct {
	db *storm.DB
}

func main() {
	var api = "YOUR API KEY" // Use Your API Key to try this program

	db, err := storm.Open(filepath.Join(".", "gallery.db"))
	if err != nil {
		log.Fatalf("%s\n", err)
	}
	defer db.Close()
	db.Init(&Data{})

	fetchData(api, db)
	fmt.Println("Done! Now you may access the server via localhost:8000")
	openUrl("http://localhost:8000")
	openUrl("http://localhost:6060/src/gallery/results.html")

	g := gin.Default()
	env := &Env{db: db}

	g.GET("/", env.queryFull)
	g.POST("/q", env.queryJSON)
	g.Static("/imgs", "./photos")
	g.Run()
}

func (e *Env) queryFull(c *gin.Context) {
	var response []Data
	e.db.All(&response)
	c.JSON(http.StatusOK, response)

	output, err := json.Marshal(response)
	jsonFile, err := os.Create("./results.json")
	if err != nil {
		panic(err)
	}
	defer jsonFile.Close()
	jsonFile.Write(output)
	jsonFile.Close()
}

func (e *Env) queryJSON(c *gin.Context) {
	var u_queries Queries

	if c.BindJSON(&u_queries) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Error occurred when parsing your JSON query ! X( "})
		return
	}

	details := make([]Data, len(u_queries))
	for i, u_query := range u_queries {
		var query []q.Matcher

		if u_query.Title != "" {
			query = append(query, q.Eq("Title", u_query.Title))
		}
		if u_query.Description != "" {
			query = append(query, q.Eq("Description", u_query.Description))
		}
		if u_query.Filename != "" {
			query = append(query, q.Eq("Filename", u_query.Filename))
		}
		if u_query.Src != "" {
			query = append(query, q.Eq("Src", u_query.Src))
		}
		var resp Data
		e.db.Select(query...).First(&resp)
		details[i] = resp
	}
	c.JSON(http.StatusOK, details)
}

func fetchData(apiKey string, db *storm.DB) {
	fetch := func(url string, page int, dest interface{}) error {
		url_page := fmt.Sprintf("%s?page=%d&client_id=%s", url, page, apiKey)
		rps, err := http.Get(url_page)
		if err != nil {
			log.Printf("%s\n", err)
			return err
		}
		defer rps.Body.Close()
		json.NewDecoder(rps.Body).Decode(dest)
		return nil
	}

	var urList CreativesSlice
	var projList UserProjectsSlice
	var rsc Project
	for i := 0; i < 10; i++ {
		fetch("https://api.behance.net/v2/creativestofollow", 1+i, &urList)
		for j, creative := range urList.Creatives {
			fetch(fmt.Sprintf("https://api.behance.net/v2/users/%s/projects", creative.Username), 1, &projList)
			fetch(fmt.Sprintf("https://api.behance.net/v2/projects/%d", projList.Projects[0].ID), 1, &rsc)

			data := Data{
				Title:       rsc.Project.Title,
				Description: rsc.Project.Description,
				Filename:    getFilename(rsc.Project.Src["original"].(string)),
				Src:         rsc.Project.Src["original"].(string),
			}

			fmt.Printf("Fetching and populating...  %d / 100\n", i*10+j)
			go fetchPhotos(data.Src)
			go db.Save(&data)
		}
	}
}

func fetchPhotos(src string) error {
	client := http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.DialTimeout(network, addr, 3*time.Second)
			},
		},
	}

	resp, err := client.Get(src)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bt, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	dir := filepath.Join(".", "photos")
	os.MkdirAll(dir, os.ModePerm)

	fname := getFilename(src)
	ioutil.WriteFile(filepath.Join(dir, fname), bt, 0644)
	return nil
}

func getFilename(src string) string {
	url := strings.Split(src, "/")
	if len(url) < 1 {
		log.Fatalf("invalid url %s\n", src)
	}
	return url[len(url)-1]
}

func openUrl(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Fatal(err)
	}
}
