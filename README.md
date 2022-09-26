# Caddy Proxy Manager - CPM

## Shoutout
Much was copied from the [original Nginx Proxy Manager](https://github.com/NginxProxyManager/nginx-proxy-manager) and implemented for Caddy. The complete basic idea therefore goes back to the repo of [jc21](https://github.com/jc21) and so the honor goes to him! So please have a look at his repo and his Nginx Proxy Manager too!

<br>

## Idea
This project tries to implement the basic idea of the Nginx Proxy Manager for Caddy and thus provide a web interface for Caddy.

Currently the version is completely unstable and untidy.

Caddy is installed normally on the system and integrates further Caddyfiles via Caddyfile. So a hotreload and caddyfiles per host is possible.



## Current features
- Adding hosts with multiple domains/upstreams
- Delete hosts

## Planned features
- Login with third-party Services like Authelia, Keycloak etc.
- Editing hosts
- Logview
- Manage Plugins

## FAQ

> Why don't you use the API from Caddy itself?

The API of Caddy is not documented and quite complicated for a simple web interface. Many features like HSTS, HTTP/2, SSL etc. are already included in Caddy and don't need to be specially configured.

> How can I use CPM?

You have to compile CPM yourself. The frontend is based on ReactJS with Typescript, the compiled frontend must then be added under ``backend/assets``. The backend is written in GoLang and can be easily compiled using ``go build cmd/main.go``.

## Contribution
If you want to help with the development pull requests etc. are welcome!