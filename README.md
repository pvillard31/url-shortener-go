# url-shortener-go

The objective of this application is to provide a RESTFull API URL Shortener in Go.

It provides the following functionalities :
- Shorten URL (with possible customization)
- Redirect to original URL when request short URL
- Provide the access count on a short URL

### Configuration file

The configuration file is conf.yml :

    // Port on which the application is listening
    httpport: 9999
    // IP Address on which the application is listening
    httplisten: 0.0.0.0
    // IP Adress for link redirection when redirecting to a long URL from a short URL
    httpaddress: 127.0.0.1
    // Page to display in case of error
    filenotfound: http://www.404notfound.fr/assets/images/pages/img/githubdotcom.jpg
    // Address of the REDIS instance
    redisaddress: redis:6379
    // Database in REDIS (default)
    redisdatabase: 0
    // Password to access REDIS database
    redispassword:
    // Length of the short URL
    codelength: 6
    // Lifetime of a shortened URL
    expirationdays: 90
    // Boolean to strictly limit (or not) the length of the short URL in case of customization
    strictlength: false

### Deployment

The application can be deployed using Docker :

Start Docker service

    service docker start
    
Pull REDIS Docker image
    
    docker pull redis
    
Run a REDIS Docker contianer
    
    docker run --name my-redis -d redis

Clone this repository

    git clone https://github.com/pvillard31/url-shortener-go
    
Go in the repository and build the Docker image of this application
    
    cd url-shortener-go
    docker build -t urlshortener .
    
Run the container of this application and link it to the REDIS container
    
    docker run -it --rm -p 9999:9999 --link my-redis:redis --name urlshortener urlshortener

### Usage

It is now possible to use the application :

> http://127.0.0.1/shortlink/www.google.fr

Will redirect to http://127.0.0.1/admin/abcdef where "abcdef" is the token.

It is possible to customize the token :

> http://127.0.0.1/shortlink/www.google.fr&custom=google

Will redirect to http://127.0.0.1/admin/google where "google" is the token (if this token is free).

If the token is not free and if there is no strict length limitation, the token will look like google123.
If the token is not free and if there is a strict length limitation, the token will look like goo123.

> http://127.0.0.1/google

Will redirect to the long URL registered with the token "google".
