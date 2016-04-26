// radiotgeek project main.go
package main

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/SlyMarbo/rss"
	"golang.org/x/net/html"
)

type Task struct {
	FileName string
	Attempt  int
	URL      string
}

type FeedBack int

var (
	ErrContentUrlNotFound = errors.New("content url not found")
	ErrInvalidFeed        = errors.New("too much or not enough <audio> in feed item")
	Respawn               = FeedBack(0)
	Success               = FeedBack(1)
	Unavailable           = FeedBack(2)
)

func main() {
	feed, err := rss.Fetch("http://www.radio-t.com/podcast-archives.rss")
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	buf := &bytes.Buffer{}
	audiourls := map[string]string{}
	audiourl := ""
	for _, item := range feed.Items {
		if item.Date.Day() <= 7 {
			buf.Reset()
			_, err = buf.WriteString(item.Content)
			if err != nil {
				log.Println(err)
				continue
			}
			audiourl, err = ParseContent(buf)
			if err != nil {
				log.Println(err)
				continue
			} else {
				audiourls[item.Title] = audiourl
			}
		}
	}
	tasks := make(chan Task, len(audiourls))
	feedback := make(chan FeedBack, 10)
	for i := 0; i < 2; i++ {
		SpawnWorker(tasks, feedback)
	}
	for fn, u := range audiourls {
		tasks <- Task{
			Attempt:  0,
			URL:      u,
			FileName: fn,
		}
	}
	dowloaded := 0
	avalaible := len(audiourls) - 1
	for r := range feedback {
		switch r {
		case Respawn:
			SpawnWorker(tasks, feedback)
			break
		case Success:
			dowloaded++
			if dowloaded == avalaible {
				log.Println("All downloads ended!\n")
				close(tasks)
				close(feedback)
			}
			break
		case Unavailable:
			avalaible--
			break
		}

	}
}

func SpawnWorker(t chan Task, fb chan FeedBack) {
	go func(tasks chan Task, feedback chan FeedBack) {
		var resp *http.Response
		var err error
		var file *os.File
		defer func() {
			val := recover()
			if val != nil {
				log.Printf("Worker dead: %v\n", val)
				log.Printf("Unavailable\n")
				feedback <- Unavailable
				feedback <- Respawn
			}
		}()
		for task := range tasks {
			log.Printf("Start downloading %q\n", task.FileName)
			if task.Attempt > 10 {
				log.Printf("Failed download %q\n", task.FileName)
				continue
			} else if task.Attempt > 0 {
				time.Sleep(4 * time.Second)
			}
			resp, err = http.Get(task.URL)
			if err != nil {
				task.Attempt++
				tasks <- task
				log.Println(err)
				continue
			}
			file, err = os.Create(task.FileName + ".mp3")
			if err != nil {
				log.Println(err)
				continue
			}
			defer file.Close()
			_, err = io.Copy(file, resp.Body)
			if err != nil {
				log.Println(err)
				continue
			}
			log.Printf("%q downloaded!\n", task.FileName)
			feedback <- Success
		}
	}(t, fb)
}

func ParseContent(r io.Reader) (string, error) {
	d := html.NewTokenizer(r)
	for {
		// token type
		tokenType := d.Next()
		if tokenType == html.ErrorToken {
			return "", d.Err()
		}
		token := d.Token()
		if tokenType == html.StartTagToken && token.Data == "audio" {
			if len(token.Attr) == 1 {
				return token.Attr[0].Val, nil
			} else {
				return "", ErrInvalidFeed
			}
		}
	}
	return "", ErrContentUrlNotFound
}
