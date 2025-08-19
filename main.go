package main

import (
	"sync"
	"fmt"
	"net/http"
	"net/url"
	"io"
	"golang.org/x/net/html"
	"strings"
	"bytes"
	"os"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"log"
	"context"
	"github.com/joho/godotenv"
)


type Queue struct {
	elements []string
	mu       sync.Mutex
}


func (q *Queue) Enqueue(url string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.elements = append(q.elements, url)
}


func (q *Queue) Dequeue() (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.elements) == 0 {
		return "", fmt.Errorf("queue is empty")
	}

	url := q.elements[0]
	q.elements = q.elements[1:] // This re-slicing is a simple way to dequeue
	return url, nil
}

// Size returns the current number of elements in the queue.
func (q *Queue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.elements)
}

type Fetecher interface{
	Fetch(url string) (body string,urls []string,err error)
}

type RealFetcher struct{}

func (r RealFetcher) Fetch(url string) (string,[]string,error){
	res , err := http.Get(url)
	if err != nil{
		return "" , nil ,err
		} 
	defer res.Body.Close()
	BodyParser,_ := io.ReadAll(res.Body)

	links,errLinks := extractLink(bytes.NewReader(BodyParser), url)

	// fmt.Printf("body: %s",string(BodyParser))
	fmt.Print("\n")

	title, _ := extractTitle(BodyParser)
    fmt.Println("Title:", title)
	text, _ := extractText(BodyParser)
    fmt.Println("Description:", text)
	fmt.Print("\n")

	// if errBody!=nil {
	// 	return "",nil,errBody
	// } 
	// bodyString := string(BodyParser)

	if errLinks!= nil{
		return "",nil,errLinks
	}
	// fmt.Print(links)
	return "",links,nil
}

func extractLink(body io.Reader, baseURL string) ([]string, error) {
	var links []string
	doc, err := html.Parse(body)
	if err != nil {
		return nil, err
	}

	base, _ := url.Parse(baseURL)

	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					href := attr.Val
					u, err := url.Parse(href)
					if err != nil {
						continue
					}
					resolved := base.ResolveReference(u) 
					links = append(links, resolved.String())
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)

	return links, nil
}
func extractTitle(body []byte) (string, error) {
    doc, err := html.Parse(bytes.NewReader(body))
    if err != nil {
        return "", err
    }
    var title string
    var f func(*html.Node)
    f = func(n *html.Node) {
        if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
            title = n.FirstChild.Data
            return
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            f(c)
        }
    }
    f(doc)
    return title, nil
}

func extractText(body []byte) (string, error) {
    doc, err := html.Parse(bytes.NewReader(body))
    if err != nil {
        return "", err
    }

    var buf strings.Builder
    var f func(*html.Node)
    f = func(n *html.Node) {
		if n.Type == html.CommentNode {
			return
		}

		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if len(body)>5 && string(body[:5])== "%PDF-" {
        fmt.Println("PDF detected from file signature")
        	return 
    }

        if n.Type == html.TextNode {
            // Add text with trimming to avoid excess spaces
            text := strings.TrimSpace(n.Data)
            if text != "" {
                buf.WriteString(text + " ")
            }
        }
        for c := n.FirstChild; c != nil; c = c.NextSibling {
            f(c)
        }
    }
    f(doc)

    return strings.TrimSpace(buf.String()), nil
}

type VisitedUrls struct{
	mu sync.Mutex
	Visited map[string]bool 
}

func (V *VisitedUrls) Visit(rawUrl string) bool{
	url := NormalizeUrl(rawUrl)
	V.mu.Lock()
	defer V.mu.Unlock()
	if V.Visited[url]{
		return false
	}
	V.Visited[url] = true
	return true
}

func SerialCrawler(seedUrl string,fetcher Fetecher,visited *VisitedUrls,q *Queue){
	q.Enqueue(seedUrl)
	PageCrawled:=0
	for q.Size() > 0{
		if PageCrawled >= 400 {
			break
		}
	
		url,err:=q.Dequeue()
		if err!= nil {
			break
		}
		if !visited.Visit(url){
			continue
		}
		_,urls,err:=fetcher.Fetch(url)
		if err!=nil{
			continue
		}
		for _,u := range urls{
		if strings.HasPrefix(u, "http") { 
		q.Enqueue(u)
		}
	}
		fmt.Printf(" %d: %s\n",PageCrawled, url)
		PageCrawled++
	}
}
func NormalizeUrl(rawURL string) string{
	u, err := url.Parse(rawURL)
	if err!=nil{
		return rawURL
	}
	u.Fragment =  ""
	u.RawQuery = ""
	return u.String()
}



func connectMongo(){
	var uri string
	err := godotenv.Load()
	if err!=nil{
		log.Fatal("Error while etching env")
	}

	if uri = os.Getenv("MONGODB_URI"); uri == "" {
		log.Fatal("You must set your 'MONGODB_URI' environment variable. See\n\t https://docs.mongodb.com/drivers/go/current/usage-examples/")
	}
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts := options.Client().ApplyURI(uri).SetServerAPIOptions(serverAPI)
	client,err := mongo.Connect(opts)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err = client.Disconnect(context.TODO()); err != nil {
			panic(err)
		}
	}()
	var result bson.M
	if err := client.Database("admin").RunCommand(context.TODO(), bson.D{{"ping", 1}}).Decode(&result); err != nil {
		panic(err)
	}
	fmt.Println("Pinged your deployment. You successfully connected to MongoDB!")

}
func main() {
	connectMongo()
	q := &Queue{
		elements: make([]string, 0),
	}
	visited := &VisitedUrls{
		Visited: make(map[string]bool),
	}
	fetcher := RealFetcher{}


	// 2. Start the crawl
	startUrl := "https://nerist.ac.in/"
	fmt.Printf("--- Starting Serial Crawl from %s ---\n", startUrl)
	SerialCrawler(startUrl, fetcher, visited,q)	
	fmt.Println("--- Serial Crawl Finished ---")

	// for i:=0;i<10;i++ {
	// 	resp , _ := http.Get("https://pkg.go.dev/net/http")
	// 	defer resp.Body.Close()
	// 	body,_:=io.ReadAll(resp.Body)
	// 	bs:=string(body)
	// 	fmt.Printf("Visited:",bs)
	// }
}