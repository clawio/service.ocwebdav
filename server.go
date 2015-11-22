package main

import (
	"encoding/json"
	"fmt"
	authlib "github.com/clawio/service.auth/lib"
	authpb "github.com/clawio/service.ocwebdav/proto/auth"
	metapb "github.com/clawio/service.ocwebdav/proto/metadata"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	dirPerm = 0755
)

var (
	statusURL       = path.Join(endPoint, "/status.php")
	remoteURL       = path.Join(endPoint, "/remote.php/webdav")
	capabilitiesURL = path.Join(endPoint, "/ocs/v1.php/cloud/capabilities")
)

type newServerParams struct {
	authServer   string
	dataServer   string
	metaServer   string
	prop         string
	sharedSecret string
}

func newServer(p *newServerParams) (*server, error) {

	s := &server{}
	s.p = p

	return s, nil
}

type server struct {
	p *newServerParams
}

func (s *server) ServeHTTPC(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	traceID := getTraceID(r)
	reqLogger := log.WithField("trace", traceID)
	ctx = NewLogContext(ctx, reqLogger)
	ctx = newGRPCTraceContext(ctx, traceID)

	reqLogger.WithField("url", r.URL.String()).Info()

	if strings.HasPrefix(r.URL.Path, statusURL) && strings.ToUpper(r.Method) == "GET" {
		reqLogger.WithField("op", "status").Info()
		s.status(ctx, w, r)
	} else if strings.HasPrefix(r.URL.Path, capabilitiesURL) && strings.ToUpper(r.Method) == "GET" {
		reqLogger.WithField("op", "capabilities").Info()
		s.capabilities(ctx, w, r)
	} else if strings.HasPrefix(r.URL.Path, remoteURL) && strings.ToUpper(r.Method) == "HEAD" {
		reqLogger.WithField("op", "head").Info()
		s.authHandler(ctx, w, r, s.head)
	} else if strings.HasPrefix(r.URL.Path, remoteURL) && strings.ToUpper(r.Method) == "PROPFIND" {
		reqLogger.WithField("op", "propfind").Info()
		s.authHandler(ctx, w, r, s.propfind)
	} else if strings.HasPrefix(r.URL.Path, remoteURL) && strings.ToUpper(r.Method) == "GET" {
		reqLogger.WithField("op", "get").Info()
		s.authHandler(ctx, w, r, s.get)
	} else if strings.HasPrefix(r.URL.Path, remoteURL) && strings.ToUpper(r.Method) == "PUT" {
		reqLogger.WithField("op", "put").Info()
		s.authHandler(ctx, w, r, s.put)
	} else if strings.HasPrefix(r.URL.Path, remoteURL) && strings.ToUpper(r.Method) == "LOCK" {
		reqLogger.WithField("op", "lock").Info()
		s.authHandler(ctx, w, r, s.lock)
	} else if strings.HasPrefix(r.URL.Path, remoteURL) && strings.ToUpper(r.Method) == "OPTIONS" {
		reqLogger.WithField("op", "options").Info()
		s.authHandler(ctx, w, r, s.options)
	} else if strings.HasPrefix(r.URL.Path, remoteURL) && strings.ToUpper(r.Method) == "MKCOL" {
		reqLogger.WithField("op", "mkcol").Info()
		s.authHandler(ctx, w, r, s.mkcol)
	} else if strings.HasPrefix(r.URL.Path, remoteURL) && strings.ToUpper(r.Method) == "MKCOL" {
		reqLogger.WithField("op", "proppatch").Info()
		s.authHandler(ctx, w, r, s.proppatch)
	} else {
		w.WriteHeader(http.StatusNotFound)
		return
	}
}

func (s *server) proppatch(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	return
}
func (s *server) mkcol(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	logger := MustFromLogContext(ctx)

	p := getPathFromReq(r)

	logger.Infof("path is %s", p)

	con, err := getConnection(s.p.metaServer)
	if err != nil {
		logger.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	defer con.Close()

	client := metapb.NewMetaClient(con)

	in := &metapb.MkdirReq{}
	in.AccessToken = authlib.MustFromTokenContext(ctx)
	in.Path = p

	_, err = client.Mkdir(ctx, in)
	if err != nil {
		logger.Error(err)

		gErr := grpc.Code(err)
		switch {
		case gErr == codes.PermissionDenied:
			http.Error(w, "", http.StatusForbidden)
			return
		default:
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *server) capabilities(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	capabilities := `
	{
	  "ocs": {
	    "data": {
	      "capabilities": {
	        "core": {
	          "pollinterval": 60
	        },
	        "files": {
	          "bigfilechunking": true,
	          "undelete": false,
	          "versioning": false
	        }
	      },
	      "version": {
	        "edition": "",
	        "major": 8,
	        "micro": 7,
	        "minor": 0,
	        "string": "8.0.7"
	      }
	    },
	    "meta": {
	      "message": null,
	      "status": "ok",
	      "statuscode": 100
	    }
	  }
	}`

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(capabilities))
}

func (s *server) status(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	logger := MustFromLogContext(ctx)

	major := "8"
	minor := "1"
	micro := "2"
	edition := ""

	version := fmt.Sprintf("%s.%s.%s.3", major, minor, micro)
	versionString := fmt.Sprintf("%s.%s.%s", major, minor, micro)

	status := &struct {
		Installed     bool   `json:"installed"`
		Maintenace    bool   `json:"maintenance"`
		Version       string `json:"version"`
		VersionString string `json:"versionstring"`
		Edition       string `json:"edition"`
	}{
		true,
		false,
		version,
		versionString,
		edition,
	}

	statusJSON, err := json.MarshalIndent(status, "", "    ")
	if err != nil {
		logger.Error(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(statusJSON)
}

func (s *server) head(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	logger := MustFromLogContext(ctx).WithField("op", "head")

	p := getPathFromReq(r)

	logger.Info("path is %s", p)

	meta, err := getMeta(ctx, s.p.metaServer, p, false)
	if err != nil {
		logger.Error(err)

		gErr := grpc.Code(err)
		switch {
		case gErr == codes.NotFound:
			http.Error(w, "", http.StatusNotFound)
			return
		case gErr == codes.PermissionDenied:
			http.Error(w, "", http.StatusForbidden)
			return
		default:
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}

	logger.Debugf("meta is %s", meta)

	w.Header().Set("Content-Type", meta.MimeType)
	w.Header().Set("ETag", meta.Etag)
	w.Header().Set("OC-FileId", meta.Id)
	w.Header().Set("OC-ETag", meta.Etag)
	t := time.Unix(int64(meta.Modified), 0)
	lastModifiedString := t.Format(time.RFC1123)
	w.Header().Set("Last-Modified", lastModifiedString)
}

func (s *server) lock(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	logger := MustFromLogContext(ctx).WithField("op", "lock")

	p := getPathFromReq(r)

	logger.Infof("path is %s", p)

	xml := `<?xml version="1.0" encoding="utf-8"?>
	<prop xmlns="DAV:">
		<lockdiscovery>
			<activelock>
				<allprop/>
				<timeout>Second-604800</timeout>
				<depth>Infinity</depth>
				<locktoken>
				<href>opaquelocktoken:00000000-0000-0000-0000-000000000000</href>
				</locktoken>
			</activelock>
		</lockdiscovery>
	</prop>`

	w.Header().Set("Content-Type", "text/xml; charset=\"utf-8\"")
	w.Header().Set("Lock-Token",
		"opaquelocktoken:00000000-0000-0000-0000-000000000000")
	w.Write([]byte(xml))
}

func (s *server) propfind(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	logger := MustFromLogContext(ctx)

	var children bool
	depth := r.Header.Get("Depth")
	// TODO(labkode) Check default for infinity header
	if depth == "1" {
		children = true
	}

	p := getPathFromReq(r)

	logger.Infof("path is %s", p)

	meta, err := getMeta(ctx, s.p.metaServer, p, children)
	if err != nil {
		logger.Error(err)

		gErr := grpc.Code(err)
		switch {
		case gErr == codes.NotFound:
			http.Error(w, "", http.StatusNotFound)
			return
		case gErr == codes.PermissionDenied:
			http.Error(w, "", http.StatusForbidden)
			return
		default:
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}

	logger.Debugf("meta is %s", meta)

	xml, err := metaToXML(meta)
	if err != nil {
		logger.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.Header().Set("DAV", "1, 3, extended-mkcol")
	w.Header().Set("ETag", meta.Etag)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(207)

	w.Write(xml)
}

func (s *server) get(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	logger := MustFromLogContext(ctx)

	p := getPathFromReq(r)

	logger.Infof("path is %s", p)

	meta, err := getMeta(ctx, s.p.metaServer, p, false)
	if err != nil {
		logger.Error(err)

		gErr := grpc.Code(err)
		switch {
		case gErr == codes.NotFound:
			http.Error(w, "", http.StatusNotFound)
			return
		case gErr == codes.PermissionDenied:
			http.Error(w, "", http.StatusForbidden)
			return
		default:
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}

	logger.Debugf("meta is %s", meta)

	c := &http.Client{}
	req, err := http.NewRequest("GET", s.p.dataServer+p, nil)
	if err != nil {
		logger.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	req.Header.Add("Authorization", "Bearer "+authlib.MustFromTokenContext(ctx))
	req.Header.Add("CIO-TraceID", logger.Data["trace"].(string))

	res, err := c.Do(req)
	if err != nil {
		log.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		http.Error(w, "", res.StatusCode)
		return
	}

	w.Header().Set("Content-Type", meta.MimeType)
	w.Header().Set("ETag", meta.Etag)
	w.Header().Set("OC-FileId", meta.Id)
	w.Header().Set("OC-ETag", meta.Etag)
	t := time.Unix(int64(meta.Modified), 0)
	lastModifiedString := t.Format(time.RFC1123)
	w.Header().Set("Last-Modified", lastModifiedString)

	io.Copy(w, res.Body)
}

func (s *server) put(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	logger := MustFromLogContext(ctx)

	p := getPathFromReq(r)

	logger.Infof("path is %s", p)

	c := &http.Client{}
	req, err := http.NewRequest("PUT", s.p.dataServer+path.Join("/", p), r.Body)
	if err != nil {
		logger.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	req.Header.Add("Authorization", "Bearer "+authlib.MustFromTokenContext(ctx))
	req.Header.Add("CIO-Checksum", r.Header.Get("CIO-Checksum"))
	req.Header.Add("CIO-TraceID", logger.Data["trace"].(string))

	res, err := c.Do(req)
	if err != nil {
		log.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	defer res.Body.Close()

	w.WriteHeader(res.StatusCode)
}

func (s *server) options(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	logger := MustFromLogContext(ctx)

	p := getPathFromReq(r)

	logger.Infof("path is %s", p)

	meta, err := getMeta(ctx, s.p.metaServer, p, false)
	if err != nil {
		logger.Error(err)

		gErr := grpc.Code(err)
		switch {
		case gErr == codes.NotFound:
			http.Error(w, "", http.StatusNotFound)
			return
		case gErr == codes.PermissionDenied:
			http.Error(w, "", http.StatusForbidden)
			return
		default:
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}

	logger.Debugf("meta is %s", meta)

	allow := "OPTIONS, LOCK, GET, HEAD, POST, DELETE, PROPPATCH, COPY,"
	allow += " MOVE, UNLOCK, PROPFIND"
	if !meta.IsContainer {
		allow += ", PUT"

	}

	w.Header().Set("Allow", allow)
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
	//w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusOK)
	return
}

// authHandler validates the access token sent in the Cookie or if not present sends
// a the Basic Auth params to the auth service to authenticate the request.
func (s *server) authHandler(ctx context.Context, w http.ResponseWriter, r *http.Request,
	next func(ctx context.Context, w http.ResponseWriter, r *http.Request)) {

	logger := MustFromLogContext(ctx)

	idt, err := getIdentityFromReq(r, s.p.sharedSecret)
	if err == nil {

		logger.Info(idt)

		ctx = authlib.NewContext(ctx, idt)
		ctx = authlib.NewTokenContext(ctx, getTokenFromReq(r))
		next(ctx, w, r)
	} else {
		// Authenticate against auth service
		// if basic credentials are found
		user, pass, ok := r.BasicAuth()
		if !ok {
			logger.Error("no credentials found in request")
			w.Header().Set("WWW-Authenticate", "Basic Realm='ClawIO credentials'")
			http.Error(w, "", http.StatusUnauthorized)
			return
		}

		con, err := getConnection(s.p.authServer)
		if err != nil {
			logger.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		defer con.Close()

		logger.Infof("connected to %s", s.p.authServer)

		client := authpb.NewAuthClient(con)

		in := &authpb.AuthRequest{}
		in.Username = user
		in.Password = pass

		res, err := client.Authenticate(ctx, in)
		if err != nil {
			logger.Error(err)

			if grpc.Code(err) == codes.Unauthenticated {
				w.Header().Set("WWW-Authenticate", "Basic Realm='ClawIO credentials'")
				http.Error(w, "", http.StatusUnauthorized)
				return
			}

			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		logger.Infof("basic auth successful for username %s", user)
		// TODO(labkode) Set cookie

		idt, err := authlib.ParseToken(res.Token, s.p.sharedSecret)
		if err != nil {
			logger.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		logger.Info(idt)

		// token added to the request because when proxied
		// no all servers will handle basic auth, but all will handle
		// the access token
		r.Header.Set("Authorization", "Bearer "+res.Token)

		ctx = authlib.NewContext(ctx, idt)
		ctx = authlib.NewTokenContext(ctx, res.Token)
		next(ctx, w, r)
	}
}
