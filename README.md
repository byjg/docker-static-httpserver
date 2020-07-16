# docker-static-httpserver

A really minimal HTTP Server for static files written in GO

## Why?

* Create a simple HTML website;
* HTTP to serve few files
* Really small - less than 15MB !!

## How to use?

```
docker run -p 8080:8080 -v /path/to/local/html:/static byjg/static-httpserver
```
