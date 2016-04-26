// radiotgeek project main.go
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
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
	AlreadyExists         = FeedBack(3)
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU() + 2)
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
	if len(audiourls) == 0 {
		log.Println("Feed unavailable")
		os.Exit(1)
	}
	tasks := make(chan Task, len(audiourls))
	feedback := make(chan FeedBack, len(audiourls))
	logchan := make(chan string, len(audiourls))

	for i := 0; i < runtime.NumCPU(); i++ {
		SpawnWorker(tasks, feedback, logchan)
	}
	log.Printf("%d workers spawned!\n", runtime.NumCPU())
	for fn, u := range audiourls {
		tasks <- Task{
			Attempt:  0,
			URL:      u,
			FileName: fn,
		}
	}
	dowloaded := 0
	avalaible := len(audiourls) - 1
	var r FeedBack
	var l string
	for {
		select {
		case r = <-feedback:
			switch r {
			case Respawn:
				SpawnWorker(tasks, feedback, logchan)
				break
			case Success:
				dowloaded++
				if dowloaded == avalaible {
					log.Println("All downloads ended!\n")
					close(tasks)
					close(feedback)
				}
				break
			case Unavailable, AlreadyExists:
				avalaible--
				break
			}
		case l = <-logchan:
			log.Printf("%s", l)
		}

	}
}

func SpawnWorker(t chan Task, fb chan FeedBack, l chan string) {
	go func(tasks chan Task, feedback chan FeedBack, logchan chan string) {
		var resp *http.Response
		var err error
		var file *os.File
		filename := ""
		defer func() {
			val := recover()
			if val != nil {
				logchan <- fmt.Sprintf("Worker dead: %v\n", val)
				feedback <- Unavailable
				feedback <- Respawn
			}
		}()
		for task := range tasks {
			logchan <- fmt.Sprintf("Start downloading %q\n", task.FileName)
			if task.Attempt > 10 {
				logchan <- fmt.Sprintf("Failed download %q. Too much attempts\n", task.FileName)
				feedback <- Unavailable
				continue
			} else if task.Attempt > 0 {
				logchan <- fmt.Sprintln("Sllep for a while")
				time.Sleep(4 * time.Second)
			}
			filename = task.FileName + ".mp3"
			if _, err := os.Stat(filename); err == nil {
				logchan <- fmt.Sprintf("%q already exists!\n", filename)
				feedback <- AlreadyExists
				continue
			}
			resp, err = http.Get(task.URL)
			if err != nil {
				task.Attempt++
				tasks <- task
				logchan <- fmt.Sprintf("Failed download %q\n%v", task.FileName, err)
				continue
			}
			file, err = os.Create(filename)
			if err != nil {
				log.Println(err)
				feedback <- Unavailable
				continue
			}
			_, err = io.Copy(file, resp.Body)
			if err != nil {
				task.Attempt++
				tasks <- task
				log.Printf("Faild download %q\n%v", task.FileName, err)
				err = file.Close()
				if err != nil {
					log.Println(err)
				}
				continue
			}
			err = file.Close()
			if err != nil {
				log.Println(err)
			}
			log.Printf("%q downloaded!\n", task.FileName)
			feedback <- Success
		}
	}(t, fb, l)
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
			if len(token.Attr) == 2 {
				return token.Attr[0].Val, nil
			} else {
				log.Println(token.Attr)
				return "", ErrInvalidFeed
			}
		}
	}
	return "", ErrContentUrlNotFound
}
