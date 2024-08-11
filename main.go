package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"

	"github.com/federicotdn/hyperlsp/lsp"
)

const idHeader = "X-LSP-Id"

func baseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		slog.Info("HTTP request", "method", req.Method, "path", req.URL.Path, "lsp_method", req.PathValue("method"))
		next.ServeHTTP(w, req)
	})
}

func errorResponse(id string, code int, message string, data ...any) *lsp.Response {
	resp := &lsp.Response{
		Id: id,
		Error: &lsp.ResponseError{
			Code:    code,
			Message: message,
		},
	}

	if len(data) > 0 {
		resp.Error.Data = data[0]
	}

	return resp
}

func handleRequest(lspSrv *lsp.Server, w http.ResponseWriter, req *http.Request) {
	pathMethod := req.PathValue("method")
	id := req.Header.Get(idHeader)
	var lspResp *lsp.Response
	var params any

	defer req.Body.Close()
	err := json.NewDecoder(req.Body).Decode(&params)
	if err != nil {
		lspResp = errorResponse("unknown", http.StatusBadRequest, "unable to unmarshal request json")
	} else if req.Method != http.MethodPost {
		lspResp = errorResponse(id, http.StatusMethodNotAllowed, "method not allowed")
	} else if pathMethod == "" {
		lspResp = errorResponse(id, http.StatusBadRequest, "no LSP method specified")
	} else {
		msg := lsp.Message{
			Id:     id,
			Method: pathMethod,
			Params: params,
		}

		var err error
		lspClient := lsp.NewClient(lspSrv)

		lspResp, err = lspClient.Send(&msg)
		if err != nil {
			lspResp = errorResponse(id, http.StatusInternalServerError, fmt.Sprintf("proxy error: %v", err))
		}
	}

	data := []byte{}
	if !lspResp.Notification {
		data, err = json.Marshal(&lspResp)
		if err != nil {
			slog.Error("unable to marshal response json", "err", err)
		}
	}

	for k, v := range lspResp.Headers {
		w.Header().Add(k, v)
	}
	if !lspResp.Notification {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	}

	if lspResp.Error != nil {
		w.WriteHeader(http.StatusBadRequest)
	} else if lspResp.Notification {
		w.WriteHeader(http.StatusNoContent)
	}

	_, err = w.Write(data)
	if err != nil {
		slog.Error("error writing response body", "err", err)
	}
}

func main() {
	addr := flag.String("addr", "localhost:8080", "Address to bind HTTP server to")
	connect := flag.String("connect", lsp.ServerConnectStdio, "Connection method to use with LSP server")
	flag.Parse()

	slog.Info("starting hyperlsp server")

	args := flag.Args()

	var lspSrv *lsp.Server
	if len(args) > 0 {
		lspSrv = lsp.NewSubprocessServer(args[0], args[1:]...)
	} else {
		lspSrv = lsp.NewExternalServer()
	}

	err := lspSrv.Connect(*connect)
	if err != nil {
		slog.Error("unable to connect to LSP server", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	srv := http.Server{Addr: *addr, Handler: mux}

	methods := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		handleRequest(lspSrv, w, req)
	})

	notfound := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	})

	mux.Handle("/lsp/{method...}", baseMiddleware(methods))
	mux.Handle("/", baseMiddleware(notfound))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)
	go func() {
		<-sig
		slog.Info("shutting down servers...")

		err := lspSrv.ShutdownAndExit()
		if err != nil {
			slog.Error("error shuttting down LSP server", "err", err)
		}
		if err := srv.Shutdown(context.Background()); err != nil {
			slog.Error("error shuttting down HTTP server", "err", err)
		}
	}()

	slog.Info("hyperlsp running", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("failed to start HTTP server", "err", err)
	}

	slog.Info("hyperlsp exit")
}
