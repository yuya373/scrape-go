package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/PuerkitoBio/goquery"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type Image struct {
	Name  string
	Bytes *bytes.Buffer
}

type Config struct {
	Pages []Page
}

type Page struct {
	Url           string
	TitleSelector string `toml:"title_selector"`
	ImageSelector string `toml:"image_selector"`
}

func (p *Page) GetTitle(doc *goquery.Document) (string, error) {
	var title = doc.Find(p.TitleSelector).Text()
	title = strings.Replace(title, "/", "_", -1)
	title = strings.Replace(title, " ", "_", -1)

	if len(title) < 1 {
		return "", errors.New("Failed to get title " + p.TitleSelector)
	}
	return title, nil
}

func (p *Page) GetDocument(url string) (*goquery.Document, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func (p *Page) GetImageSrcs(doc *goquery.Document) []string {
	images := doc.Find(p.ImageSelector)
	results := make([]string, images.Length())

	images.Each(func(i int, el *goquery.Selection) {
		src, exists := el.Attr("src")
		if exists {
			results[i] = src
		}
	})

	return results
}

func downloadImage(src string) (*Image, error) {
	if 0 < len(src) {
		res, err := http.Get(src)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()

		buf := new(bytes.Buffer)
		io.Copy(buf, res.Body)

		paths := strings.Split(src, "/")
		name := paths[len(paths)-1]

		image := Image{Name: name, Bytes: buf}
		return &image, nil
	}
	return nil, errors.New("<img> does not have attribute `src`")
}

func downloadImages(srcs []string) []*Image {
	log.Println(len(srcs), "images.")
	results := make(chan []*Image)
	finished := make(chan bool)
	done := make(chan *Image)

	go func() {
		xs := make([]*Image, 0)
		for {
			select {
			case x := <-done:
				xs = append(xs, x)
			case <-finished:
				results <- xs
				return
			}
		}
	}()

	go func() {
		var wg sync.WaitGroup
		for i, src := range srcs {
			wg.Add(1)
			go func(i int, src string) {
				log.Println("START", "[", i, "]", src)

				image, err := downloadImage(src)
				log.Println("DONE", "[", i, "]", src)

				if err != nil {
					log.Fatal(err)
				}
				name := strconv.Itoa(i) + "-" + image.Name
				image.Name = name

				done <- image
				wg.Done()
			}(i, src)
		}
		wg.Wait()
		finished <- true
	}()

	return <-results
}

func save(title string, zip *bytes.Buffer) (int, error) {
	log.Println("Create directory")
	err := os.MkdirAll("downloads", 0755)
	if err != nil {
		return 0, err
	}

	log.Println("Create zip file")
	f, err := os.Create("downloads/" + title + ".zip")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	log.Println("Write zip file")
	n, err := f.Write(zip.Bytes())
	if err != nil {
		return 0, err
	}
	log.Println("Saved", title)
	return n, nil
}

func createZip(images []*Image) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)
	defer writer.Close()

	for _, image := range images {

		w, err := writer.Create(image.Name)
		if err != nil {
			return nil, err
		}

		_, err = io.Copy(w, image.Bytes)
		if err != nil {
			return nil, err
		}
	}

	return buf, nil
}

func scrape(page *Page, url string) error {
	doc, err := page.GetDocument(url)
	if err != nil {
		return err
	}

	title, err := page.GetTitle(doc)
	if err != nil {
		return err
	}
	srcs := page.GetImageSrcs(doc)
	images := downloadImages(srcs)

	zip, err := createZip(images)
	if err != nil {
		return err
	}

	_, e := save(title, zip)

	if e != nil {
		return err
	}

	return nil
}

func cli(page Page, wg *sync.WaitGroup) error {
	for {
		fmt.Print("URL:")
		var url string

		_, err := fmt.Scanln(&url)
		if err != nil {
			return err
		}
		if len(url) < 0 {
			break
		}
		fmt.Println("→", url)

		wg.Add(1)
		go func(page *Page, url string) {
			err := scrape(page, url)
			if err != nil {
				log.Fatal(err)
			}
			wg.Done()
		}(&page, url)
	}

	return nil
}

func main() {
	var config Config
	_, err := toml.DecodeFile("config.toml", &config)
	if err != nil {
		log.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, page := range config.Pages {
		err := exec.Command(
			"open",
			"-n",
			"-a",
			"Google Chrome",
			"--args",
			"--incognito",
			page.Url,
		).Run()
		if err != nil {
			log.Fatal(err)
		}
		cli(page, &wg)
	}
	wg.Wait()
}
