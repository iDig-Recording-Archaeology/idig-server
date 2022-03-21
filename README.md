# iDig Server

iDig Server can run on Linux, macOS, Windows or any other OS supported by Go.
To build it you'll need Go 1.17 or later. Inside the source directory run:

```
go build
```

## Root Directory

Use ```-r /path/to/root``` to pass the root directory of the server. This is place where
all the trench data and other configuration are beeing stored.

## Users

When you run the server the first time it will create a users file with the default username ```idig``` and password ```idig```.
You can change this and add more users by editing the ```users``` file inside the Root Directory.

## Running iDig Server

By default, iDig Server will start an HTTP server on port 9000. This mode is insecure, as all data are sent unencrypted.
If you are planning to expose the server on the Internet, please use one of the following methods:

### With auto-generated certificates from Let's Encrypt

iDig Server can use HTTPS with auto-generated TLS certificates. For this you'll need a valid domain name that resolves
to the IP address of the server. Then you can run:

```
idig-server -s example.com -e admin@example.com
```

The email is optional but is advised to use it, so that Let's Encrypt can reach you in case of any problems with the certificate.

### Behind a Reverse Proxy

If you already run an HTTPS web server, then you can run iDig server behind a reverse proxy. In that case iDig server should only
listen for localhost connections. e.g.:

```
idig-server -p 4000
```

All the API endpoints are under the /idig/ path, to facilitate this use case.

