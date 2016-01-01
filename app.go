package main

import (
	"github.com/gorilla/mux"
	"encoding/json"
    "io/ioutil"
    "path/filepath"
    "math/rand"
	"errors"
	"os"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"regexp"
	redis "gopkg.in/redis.v3"
	log "github.com/Sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

var (
	// REDIS client
	redisclient  *redis.Client
	// Configuration of the application
	config		 *Config
	// source for random function
	src = rand.NewSource(time.Now().UnixNano())
)

// alphabet for random string generation
const letterBytes = "abcdefghijklmnopqrstuvwxyz1234567890"
const (
    letterIdxBits = 6                    // 6 bits to represent a letter index
    letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
    letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// configuration object
type Config struct {
        Httpport string
        Httplisten string
        Httpaddress string
        Filenotfound string
        Redisaddress string
        Redisdatabase int64
        Redispassword string
        Codelength int
        Expirationdays int
        Strictlength bool
}

// representation of a URL
type MyUrl struct {
	Key          string // token
	ShortUrl     string // short URL
	LongUrl      string // long URL
	CreationDate int64 // creation date of the short link
	Clicks       int64 // number of visits
}

// Function to convert the MyUrl to JSON.
func (k MyUrl) Json() []byte {
	b, _ := json.Marshal(k)
	return b
}

// Creates a new MyUrl instance. The Given key, shorturl and longurl will
// be used. Clicks will be set to 0 and CreationDate to time.Now().Unix()
func NewMyUrl(key, shorturl, longurl string) *MyUrl {
	myUrl := new(MyUrl)
	myUrl.CreationDate = time.Now().Unix()
	myUrl.Key = key
	myUrl.LongUrl = longurl
	myUrl.ShortUrl = shorturl
	myUrl.Clicks = 0
	return myUrl
}

// Stores a new MyUrl for the given key, shorturl and longurl. Existing
// ones with the same url will be overwritten (this may happen only in 
// case the link is no longer valid regarding the configured limit duration) 
func store(key, shorturl, longurl string) *MyUrl {
	myUrl := NewMyUrl(key, shorturl, longurl)
	_, err := redisclient.HSet(myUrl.Key, "LongUrl", myUrl.LongUrl).Result()
	
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("REDIS STORE ERROR")
    }
	
	_, err = redisclient.HSet(myUrl.Key, "ShortUrl", myUrl.ShortUrl).Result()
	
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("REDIS STORE ERROR")
    }
	
	_, err = redisclient.HSet(myUrl.Key, "CreationDate", strconv.FormatInt(myUrl.CreationDate, 10)).Result()
	
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("REDIS STORE ERROR")
    }
	
	_, err = redisclient.HSet(myUrl.Key, "Clicks", strconv.FormatInt(myUrl.Clicks, 10)).Result()
	
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("REDIS STORE ERROR")
    }
	
	log.WithFields(log.Fields{
		"key": key,
		"LongUrl": myUrl.LongUrl,
		"ShortUrl": myUrl.ShortUrl,
		"CreationDate": strconv.FormatInt(myUrl.CreationDate, 10),
		"Clicks": strconv.FormatInt(myUrl.Clicks, 10),
	}).Info("REDIS STORE")
	
	return myUrl
}

// Gets a MyUrl instance for the given key from REDIS. If the key is
// not found, os.Error is returned.
func load(key string) (*MyUrl, error) {
	if ok, _ := redisclient.HExists(key, "ShortUrl").Result(); ok {
		myUrl := new(MyUrl)
		myUrl.Key = key
		reply := redisclient.HMGet(key, "LongUrl", "ShortUrl", "CreationDate", "Clicks").Val()
		create, _ := strconv.ParseInt(reply[2].(string), 10, 64)
		click, _ := strconv.ParseInt(reply[3].(string), 10, 64)
		
		myUrl.LongUrl, myUrl.ShortUrl, myUrl.CreationDate, myUrl.Clicks = 
			reply[0].(string), 
			reply[1].(string), 
			create, 
			click
			
		log.WithFields(log.Fields{
		    "url": myUrl,
		  }).Info("REDIS GET")
	
		return myUrl, nil
	}
	
	log.WithFields(log.Fields{
		"key": key,
	}).Error("/admin unknown key")
		
	return nil, errors.New("unknown key: " + key)
}

// Function to display the info about a MyUrl given by it's Key.
// In particular it gives the nmber of access to the URL.
// The result is displayed in JSON format
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
		http.Redirect(w, r, config.Filenotfound, http.StatusNotFound)
	}
}

// function to resolve a shorturl and redirect
func resolve(w http.ResponseWriter, r *http.Request) {
	log.Info("Received a resolve request")
	short := mux.Vars(r)["short"]
	myUrl, err := load(short)
	
	if err == nil {
		redisclient.HIncrBy(myUrl.Key, "Clicks", 1).Result()
		http.Redirect(w, r, myUrl.LongUrl, http.StatusMovedPermanently)
	} else {
		http.Redirect(w, r, config.Filenotfound, http.StatusMovedPermanently)
	}
}

// Determines if the string rawurl is a valid URL to be stored.
func isValidUrl(rawurl string) (u *url.URL, err error) {
	if len(rawurl) == 0 {
		return nil, errors.New("empty url")
	}
	// test that the URL is active
	_, ex := http.Get("http://" + rawurl)
	if ex != nil {
		return nil, errors.New("invalid url")
	}
	return url.Parse(rawurl)
}

// Function to encode the URL. It looks for customization and
// ensure to give a free token.
func encode(input string) string {
	// if customization
	custom := input
	if custom == "" {
		custom = randSeq(config.Codelength)
	}
	
	// while key not existing, generating key
	i := 0
	random := ""
	
	// if the length of the customization is not limited
	if config.Strictlength {
		custom = custom[:config.Codelength]
	}
	
	// while the token is not free, try to generate a correct one
	// still respecting the customization
	for isNotFree(custom) && i < 100 {
		random = strconv.Itoa(rand.Intn(999))
		l := len(random)
		if config.Strictlength && l + len(custom) > config.Codelength {
			custom = custom[:config.Codelength-l] + random
		} else {
			custom = custom + random
		}
		i = i + 1
	}
	
	// everything taken... random !
	if i == 100 {
		for isNotFree(custom) {
			custom = randSeq(config.Codelength)
		}
	}
	
	// return key
	return custom
}

// check if an encoded url is free. It looks for :
// key not existing or key existing and creation date older than 3 months
func isNotFree(key string) bool {
	if ok, _ := redisclient.HExists(key, "ShortUrl").Result(); ok {
		reply := redisclient.HMGet(key, "CreationDate").Val()
		create, _ := strconv.ParseInt(reply[0].(string), 10, 64)
		return time.Now().Unix() - create < int64(config.Expirationdays * 3600)
	} else {
		return false
	}
}


// generate a random string of a given length
func randSeq(n int) string {
    b := make([]byte, n)
    for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
        if remain == 0 {
            cache, remain = src.Int63(), letterIdxMax
        }
        if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
            b[i] = letterBytes[idx]
            i--
        }
        cache >>= letterIdxBits
        remain--
    }
    return string(b)
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
	
	
	// check if there is a customisation
	re = regexp.MustCompile(`^(.*)&custom=(.*)$`)
	splits := re.FindStringSubmatch(leUrl)
	custom := ""
	// if yes
	if len(splits) > 1 {
		leUrl = splits[1]
		custom = splits[2]
	}
	
	_, err := isValidUrl(string(leUrl))
	if err == nil {
		encoded := encode(custom)
		location := fmt.Sprintf("http://%s/admin/%s", host, encoded)
		store(encoded, location, "http://" + leUrl)

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
	
		http.Redirect(w, r, config.Filenotfound, http.StatusNotFound)
	}
}

func main() {
	filename, _ := filepath.Abs(os.Args[1])
	
	// look for configuration file (YML) and unmarshal it
    yamlFile, err := ioutil.ReadFile(filename)
    if err != nil {
        panic(err)
    }
    
	err = yaml.Unmarshal(yamlFile, &config)
    if err != nil {
        panic(err)
    }
	
	// Set REDIS client instance
	host := config.Redisaddress
	db := config.Redisdatabase
	passwd := config.Redispassword

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

	// HTTP instance listening on :
	router := mux.NewRouter()
	router.HandleFunc("/shortlink/{url:(.*$)}", shorten)
	router.HandleFunc("/{short:([a-zA-Z0-9]+$)}", resolve)
	router.HandleFunc("/admin/{short:[a-zA-Z0-9]+}", info)

	// launch server
	listen := config.Httplisten
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