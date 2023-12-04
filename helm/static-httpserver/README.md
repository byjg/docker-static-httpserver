# Static Http Server

A minimal HTTP Server image for static files written in GO

This repository integrates with the [EasyHAProxy](https://github.com/byjg/docker-easy-haproxy/blob/master/README.md).

More information on [GitHub](https://github.com/byjg/docker-static-httpserver/blob/master/README.md)

## Install

```bash
helm repo add byjg https://opensource.byjg.com/helm
helm repo update byjg
helm install mysite byjg/static-httpserver \
    --namespace default \
    --set "ingress.hosts={www.example.org,example.org}" \
    --set parameters.title=Welcome
```

## Parameters

```yaml
image:
  repository: byjg/static-httpserver
  pullPolicy: IfNotPresent
  tag: ""

ingress:
  enabled: true
  className: ""
  annotations:
    kubernetes.io/ingress.class: easyhaproxy-ingress
  hosts: []

parameters:
  htmlTitle: ""
  title: "soon"
  message: ""
  backgroundImage: ""
  facebook: ""
  twitter: ""
  youtube: ""
```
