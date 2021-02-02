# Static http server

[![Opensource ByJG](https://img.shields.io/badge/opensource-byjg-success.svg)](http://opensource.byjg.com)
[![GitHub source](https://img.shields.io/badge/Github-source-informational?logo=github)](https://github.com/byjg/docker-static-httpserver/)
[![GitHub license](https://img.shields.io/github/license/byjg/docker-static-httpserver.svg)](https://opensource.byjg.com/opensource/licensing.html)

A really minimal HTTP Server image for static files written in GO

## Why?

* Create a simple HTML website;
* HTTP to serve few files
* Really small - less than 15MB !!

## Tags

- latest - The latest image with a coming soon template
- tiny - A really minimalist image. You need to replace the volume ot build your own on top of this one.  

## How to use the "Coming soon template"?

![Coming soon page](https://raw.github.com/byjg/docker-static-httpserver/master/preview.png)

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

## Use your own static pages

```
docker run -p 8080:8080 -v /path/to/local/html:/static byjg/static-httpserver:tiny
```


## Create your own image

Dockerfile

```
FROM byjg/static-httpserver:tiny

COPY /path/to/html /static
```

----
[Open source ByJG](http://opensource.byjg.com)
