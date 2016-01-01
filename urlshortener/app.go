package main

import (
	"github.com/gorilla/mux"
	"encoding/json"
    "io/ioutil"
    "path/filepath"
	"errors"
	"os"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"regexp"
	redis "gopkg.in/redis.v3"
	log "github.com/Sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	// special key in redis, that is our global counter
	COUNTER = "__counter__"
)

var (
	redisclient  *redis.Client
	config		 *Config
	filenotfound string
)

type Config struct {
        Httpport string
        Httpaddress string
        Filenotfound string
        Redisaddress string
        Redisdatabase int64
        Redispassword string
}

type MyUrl struct {
	Key          string
	ShortUrl     string
	LongUrl      string
	CreationDate int64
	Clicks       int64
}

// Converts the MyUrl to JSON.
func (k MyUrl) Json() []byte {
	b, _ := json.Marshal(k)
	return b
}

// Creates a new MyUrl instance. The Given key, shorturl and longurl will
// be used. Clicks will be set to 0 and CreationDate to time.Nanoseconds()
func NewMyUrl(key, shorturl, longurl string) *MyUrl {
	myUrl := new(MyUrl)
	myUrl.CreationDate = time.Now().UnixNano()
	myUrl.Key = key
	myUrl.LongUrl = longurl
	myUrl.ShortUrl = shorturl
	myUrl.Clicks = 0
	return myUrl
}

// stores a new MyUrl for the given key, shorturl and longurl. Existing
// ones with the same url will be overwritten
func store(key, shorturl, longurl string) *MyUrl {
	myUrl := NewMyUrl(key, shorturl, longurl)
	go redisclient.HSet(myUrl.Key, "LongUrl", myUrl.LongUrl).Result()
	go redisclient.HSet(myUrl.Key, "ShortUrl", myUrl.ShortUrl).Result()
	go redisclient.HSet(myUrl.Key, "CreationDate", strconv.FormatInt(myUrl.CreationDate, 10)).Result()
	go redisclient.HSet(myUrl.Key, "Clicks", strconv.FormatInt(myUrl.Clicks, 10)).Result()
	
	log.WithFields(log.Fields{
		"key": key,
		"LongUrl": myUrl.LongUrl,
		"ShortUrl": myUrl.ShortUrl,
		"CreationDate": strconv.FormatInt(myUrl.CreationDate, 10),
		"Clicks": strconv.FormatInt(myUrl.Clicks, 10),
	}).Error("REDIS STORE")
	
	return myUrl
}

// loads a MyUrl instance for the given key. If the key is
// not found, os.Error is returned.
func load(key string) (*MyUrl, error) {
	if ok, _ := redisclient.HExists(key, "ShortUrl").Result(); ok {
		myUrl := new(MyUrl)
		myUrl.Key = key
		reply := redisclient.HMGet(key, "LongUrl", "ShortUrl", "CreationDate", "Clicks").Val()
		myUrl.LongUrl, myUrl.ShortUrl, myUrl.CreationDate, myUrl.Clicks = 
			reply[0].(string), reply[1].(string), reply[2].(int64), reply[3].(int64)
			
		log.WithFields(log.Fields{
		    "url": myUrl,
		  }).Info("/admin request")
	
		return myUrl, nil
	}
	
	log.WithFields(log.Fields{
		"key": key,
	}).Error("/admin unknown key")
		
	return nil, errors.New("unknown key: " + key)
}

// function to display the info about a MyUrl given by it's Key
func info(w http.ResponseWriter, r *http.Request) {
	log.Info("Received a /admin request")
	
	short := mux.Vars(r)["short"]
	
	log.WithFields(log.Fields{
	    "short": short,
	  }).Info("/admin request")
	
	myUrl, err := load(short)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(myUrl.Json())
		io.WriteString(w, "\n")
	} else {
		http.Redirect(w, r, filenotfound, http.StatusNotFound)
	}
}

// function to resolve a shorturl and redirect
func resolve(w http.ResponseWriter, r *http.Request) {
	log.Info("Received a resolve request")
	short := mux.Vars(r)["short"]
	myUrl, err := load(short)
	
	if err == nil {
		go redisclient.HIncrBy(myUrl.Key, "Clicks", 1)
		http.Redirect(w, r, myUrl.LongUrl, http.StatusMovedPermanently)
	} else {
		http.Redirect(w, r, filenotfound, http.StatusMovedPermanently)
	}
}

// Determines if the string rawurl is a valid URL to be stored.
func isValidUrl(rawurl string) (u *url.URL, err error) {
	if len(rawurl) == 0 {
		return nil, errors.New("empty url")
	}
	// XXX this needs some love...
	if !strings.HasPrefix(rawurl, "http") {
		rawurl = fmt.Sprintf("http://%s", rawurl)
	}
	return url.Parse(rawurl)
}

// function to shorten and store a url
func shorten(w http.ResponseWriter, r *http.Request) {
	log.Info("Received /shortlink request")
	
	host := config.Httpaddress + ":" + config.Httpport
	url := r.URL.String()
	re := regexp.MustCompile(`^.*/shortlink/(.*)$`)
	leUrl := re.FindStringSubmatch(url)[1]
	
	log.WithFields(log.Fields{
	    "url": url,
	    "value": leUrl,
	  }).Info("/shortlink request")
	
	theUrl, err := isValidUrl(string(leUrl))
	if err == nil {
		ctr, _ := redisclient.Incr(COUNTER).Result()
		encoded := Encode(ctr)
		location := fmt.Sprintf("http://%s/admin/%s", host, encoded)
		store(encoded, location, theUrl.String())

		home := r.FormValue("home")
		
		log.WithFields(log.Fields{
		    "encoded": encoded,
		    "location": location,
		    "home": home,
		  }).Info("/shortlink request")
		
		if home != "" {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
		} else {
			// redirect to the info page
			http.Redirect(w, r, location, http.StatusMovedPermanently)
		}
	} else {
		log.WithFields(log.Fields{
		    "error": err,
		  }).Error("/shortlink error")
	
		http.Redirect(w, r, filenotfound, http.StatusNotFound)
	}
}

func main() {
	filename, _ := filepath.Abs(os.Args[1])
	
    yamlFile, err := ioutil.ReadFile(filename)
    if err != nil {
        panic(err)
    }
    
	err = yaml.Unmarshal(yamlFile, &config)
    if err != nil {
        panic(err)
    }
	
	host := config.Redisaddress
	db := config.Redisdatabase
	passwd := config.Redispassword
	filenotfound = config.Filenotfound

	redisclient = redis.NewClient(&redis.Options{
    	Addr:     host,
    	Password: passwd,
    	DB:       int64(db),
	})

	log.WithFields(log.Fields{
	    "host": host,
	    "db": db,
	    "passwd": passwd,
	  }).Info("Setting REDIS instance parameters")

	router := mux.NewRouter()
	router.HandleFunc("/shortlink/{url:(.*$)}", shorten)
	router.HandleFunc("/{short:([a-zA-Z0-9]+$)}", resolve)
	router.HandleFunc("/admin/{short:[a-zA-Z0-9]+}", info)

	listen := config.Httpaddress
	port := config.Httpport
	s := &http.Server{
		Addr:    listen + ":" + port,
		Handler: router,
	}

	log.WithFields(log.Fields{
	    "listen": listen,
	    "port": port,
	  }).Info("Setting HTTP instance")
	
	s.ListenAndServe()
}