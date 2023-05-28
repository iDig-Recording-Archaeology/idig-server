# iDig Server

iDig Server can run on Linux, macOS, Windows or any other OS supported by Go.
To build it you'll need Go 1.20 or later. Inside the source directory run:

```
go build
```

## Root Directory

Use `IDIG_SERVER_DIR` environment variable to set the root directory of the server. This is place where all the trench data and other configuration are beeing stored. If not set, the user's home directory will be used.

## Configuration

### Create a new Project

The name of the *Project* must match the `project` field in your `Preferences.json` file in iDig.

Each *Project* can contain multiple trenches and has its own list of users.

```
idig-server create Agora
```

### Add one or more users to the *Project*

```
idig-server adduser Agora bruce myPassw0rd
```

### See the list of users

```
idig-server listusers Agora
```

### Delete a user

```
idig-server deluser Agora bruce
```

## Running iDig Server

```
idig-server start
```

By default, iDig Server will start an HTTP server on port 9000. This mode is insecure, as all data are sent unencrypted. If you are planning to expose the server on the Internet, please run it behind a reverse proxy.

### Behind a Reverse Proxy

If you already run an HTTPS web server, then you can run iDig server behind a reverse proxy. In that case iDig server should only
listen for localhost connections. e.g.:

```
idig-server start -p 4000
```
