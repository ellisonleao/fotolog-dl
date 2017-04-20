package main

import (
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	uuid "github.com/satori/go.uuid"
)

const (
	fotologURL   = "http://www.fotolog.com/%s/mosaic/%s"
	outputFolder = "./images"
)

var (
	usernameFlag string
	zipFlag      bool
	pageStr      string
)

// processImage will go into the photo link and save the image
func processImage(url string) error {
	doc, err := goquery.NewDocument(url)
	if err != nil {
		return fmt.Errorf("Error on fetching %s", url)
	}

	// getting image url
	imageURL, _ := doc.Find("a.wall_img_container_big > img").Attr("src")

	// creating the image file
	filename := fmt.Sprintf(outputFolder+"/image-%s.jpg", uuid.NewV4())
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("could not create image file %s: %v", filename, err)
	}
	defer file.Close()

	// getting the image from fotolog page and saving it
	resp, err := http.Get(imageURL)
	if err != nil {
		return fmt.Errorf("could not get image %s: %v", imageURL, err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("could not save file %s: %v", filename, err)
	}

	return nil
}

// processPage will grab all image links and call the image processor
func processPage(doc *goquery.Document, wg *sync.WaitGroup) int {
	imagesProcessed := 0
	doc.Find("a.wall_img_container").Each(func(i int, s *goquery.Selection) {
		link, _ := s.Attr("href")
		wg.Add(1)
		go func(link string) {
			defer wg.Done()
			err := processImage(link)
			if err == nil {
				imagesProcessed++
			}
		}(link)
	})
	return imagesProcessed
}

// zipImages will create the zipfile for the downloaded images folder
func zipImages() error {
	// check if we got the images directory
	_, err := os.Stat(outputFolder)
	if err != nil {
		return errors.New("images folder does not exists")
	}

	// create zip file
	zipFile, err := os.Create("./images.zip")
	if err != nil {
		return err
	}

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	filepath.Walk(outputFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("Error on walking to %s: %v", path, err)
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		if info.IsDir() {
			header.Name += "/"
		}
		header.Method = zip.Store

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if header.Mode().IsRegular() {
			file, err := os.Open(path)

			if err != nil {
				return fmt.Errorf("error on opening %s: %v", path, err)
			}
			defer file.Close()

			_, err = io.CopyN(writer, file, info.Size())
			if err != nil && err != io.EOF {
				return fmt.Errorf("could not add image to zip: %v", err)
			}
		}

		return nil
	})

	return nil
}

// deleteOutputFolder will delete the current images folder after the zip creation
func deleteOutputFolder() error {
	// check if zip file exists
	_, err := os.Stat("./images.zip")
	if err != nil {
		return fmt.Errorf("images.zip file does not exists. Skipping output folder delete")
	}

	files, err := ioutil.ReadDir(outputFolder)
	if err != nil {
		return fmt.Errorf("could not read from images folder: %v", err)
	}

	for _, file := range files {
		err := os.Remove(filepath.Join(outputFolder, file.Name()))
		if err != nil {
			return fmt.Errorf("could not remove %s: %v", file.Name(), err)
		}
	}

	if err = os.Remove(outputFolder); err != nil {
		return fmt.Errorf("could not remove images folder: %v", err)
	}
	return nil
}

func init() {
	flag.StringVar(&usernameFlag, "username", "", "username")
	flag.BoolVar(&zipFlag, "zip", false, "zip images")
}

func main() {
	t0 := time.Now()
	flag.Parse()
	if len(usernameFlag) == 0 {
		fmt.Println("Please provide an username")
		os.Exit(1)
	}

	// create output dir
	if err := os.Mkdir("images", os.ModePerm); err != nil {
		if !os.IsExist(err) {
			log.Fatal(err)
		}
	}

	url := fmt.Sprintf(fotologURL, usernameFlag, "")

	// processing first page to get the last page
	doc, err := goquery.NewDocument(url)
	if err != nil {
		log.Fatalf("Error on fetching %s", url)
	}

	// syncing all goroutines
	wg := sync.WaitGroup{}

	// getting last page as an int value
	lastLink, _ := doc.Find("#pagination > a:last-child").Last().Attr("href")
	lastPage, _ := strconv.Atoi(strings.Split(lastLink, "/")[5])

	// processing first page since we already fetched it
	fmt.Println("Processing", url)
	wg.Add(1)
	go func(doc *goquery.Document) {
		defer wg.Done()
		processPage(doc, &wg)
	}(doc)

	// processing remaining pages
	// for each page we process their images
	for i := 30; i <= lastPage; i += 30 {
		wg.Add(1)
		pageStr = strconv.Itoa(i)
		url = fmt.Sprintf(fotologURL, usernameFlag, pageStr)
		fmt.Println("Processing", url)

		doc, err := goquery.NewDocument(url)
		if err != nil {
			log.Fatalf("Error on fetching %s", url)
			continue
		}

		go func(doc *goquery.Document) {
			defer wg.Done()
			processPage(doc, &wg)
		}(doc)
	}
	wg.Wait()

	if zipFlag {
		// zip images folder
		if err = zipImages(); err != nil {
			log.Fatalf("Could not create zip image file: %v", err)
		}

		// remove output folder
		if err := deleteOutputFolder(); err != nil {
			log.Fatalf("Could not remove images folder: %v", err)
		}
	}

	t1 := time.Since(t0)
	fmt.Printf("elapsed time: %.2f seconds\n", t1.Seconds())
}
