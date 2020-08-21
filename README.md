# docker-static-httpserver

A really minimal HTTP Server for static files written in GO

## Why?

* Create a simple HTML website;
* HTTP to serve few files
* Really small - less than 15MB !!

## Tags

- latest - The latest image with a coming soon template
- tiny - A really minimalist image. You need to replace the volume ot build your own on top of this one.  

## How to use?

The image has the coming soon template and can be customized by setting the environment variables:
- HTML_TITLE
- TITLE
- MESSAGE
- BG_IMAGE
- FACEBOOK
- TWITTER
- YOUTUBE

e.g.

```bash
docker run -p 8080:8080 -e TITLE=soon -e "MESSAGE=Keep In Touch" byjg/static-httpserver
```

## Changing the volume

```
docker run -p 8080:8080 -v /path/to/local/html:/static byjg/static-httpserver:tiny
```


## Create your own image

Dockerfile

```
FROM byjg/static-httpserver:tiny

COPY /path/to/html /static
```
