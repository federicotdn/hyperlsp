# HyperLSP

A small HTTP/1.1 to LSP proxy written in Go. This allows you to use your favorite HTTP tools for interacting with LSP servers, mostly for debugging or testing purposes. HyperLSP tries to map HTTP/1.1 to LSP in a practical way.

## Installation

To install, run:

```bash
$ go install github.com/federicotdn/hyperlsp@latest
```

## Usage

HyperLSP can connect to a LSP server via `stdio`, or via TCP (e.g. `localhost:1234`). This must be specified with the `-connect` flag.
Additionally, HyperLSP can also spawn an LSP server subprocess by its own. This is done if one or more positional arguments are passed to HyperLSP. In order to use the `stdio` connection method, an LSP server subprocess **must** be created.

Examples:

```bash
# Launch pylsp and connect via stdio
$ hyperlsp -connect stdio -- pylsp
```

```bash
# Launch gopls and connect via TCP
$ hyperlsp -connect localhost:9090 -- gopls -listen :9090
```

```bash
# Connect to an already running gopls server
$ hyperlsp -connect localhost:9090
```

The address HyperLSP listens at can be configured via the `-addr` flag. The default is `localhost:8080`.

Once HyperLSP is running, you can use HTTP to send and receive LSP data. All requests must be POST and use the path `/lsp/{method_name}`. The `X-LSP-Id` header must be set to a nonempty string if a response is expected (otherwise, a notification will be sent).

```http
POST /lsp/initialize
X-LSP-Id: 123

{
    "processId": null,
    "workspaceFolders": [
        {
            "uri": "file:///home/foobar/myproject
        }
    ]
}
```

```http
HTTP/1.1 200 OK
Content-Type: application/json
Content-Length: 194

{
    "id": "1234",
    "result": {...}, // optional
    "error": {...} // optional
}
```

The following HTTP codes are returned:
- `200 OK`: A response to a request, without an error.
- `204 No Content`: An (empty) response to a notification.
- `400 Bad Request`: A response to a request, with an error present. May also be returned if the HTTP client did not send valid JSON data, or did not specify a method in the path.
- `405 Method Not Allowed`: HTTP client did not use POST.
- `500 Internal Server Error`: Error encountered when communicating with the LSP server, or when parsing its response.

## License

Distributed under the Apache-2.0 license. See [LICENSE](LICENSE) for more information.
