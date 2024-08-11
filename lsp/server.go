package lsp

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os/exec"
	"sync"
	"syscall"
)

const ServerConnectStdio = "stdio"

type Server struct {
	cmd   *exec.Cmd
	mutex *sync.Mutex
	conn  serverConn
}

type serverConn interface {
	read([]byte) (int, error)
	readErr([]byte) (int, error)
	write([]byte) (int, error)
	close() error
}

type serverConnPipe struct {
	stdout io.ReadCloser
	stderr io.ReadCloser
	stdin  io.WriteCloser
}

type serverConnTCP struct {
	conn net.Conn
}

func newServerConnPipe(cmd *exec.Cmd) (*serverConnPipe, error) {
	if cmd == nil {
		return nil, fmt.Errorf("no LSP server subprocess present")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("unable to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("unable to create stderr pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("unable to create stdin pipe: %w", err)
	}

	return &serverConnPipe{
		stdout: stdout,
		stderr: stderr,
		stdin:  stdin,
	}, nil
}

func (c *serverConnPipe) read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *serverConnPipe) readErr(p []byte) (int, error) {
	return c.stderr.Read(p)
}

func (c *serverConnPipe) write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *serverConnPipe) close() error {
	return errors.Join(
		c.stdout.Close(),
		c.stderr.Close(),
		c.stdin.Close(),
	)
}

func newServerConnTCP(addr string) (*serverConnTCP, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	return &serverConnTCP{conn: conn}, nil
}

func (c *serverConnTCP) read(p []byte) (int, error) {
	return c.conn.Read(p)
}

func (c *serverConnTCP) readErr(p []byte) (int, error) {
	return 0, io.EOF
}

func (c *serverConnTCP) write(p []byte) (int, error) {
	return c.conn.Write(p)
}

func (c *serverConnTCP) close() error {
	return c.conn.Close()
}

func NewSubprocessServer(name string, arg ...string) *Server {
	cmd := exec.Command(name, arg...)

	// Prevent Ctrl-C from killing LSP server in Linux.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	srv := NewExternalServer()
	srv.cmd = cmd
	return srv
}

func NewExternalServer() *Server {
	return &Server{
		mutex: &sync.Mutex{},
	}
}

func (s *Server) forwardStderr() {
	buf := make([]byte, 4096)
	for {
		n, err := s.conn.readErr(buf)
		if n > 0 {
			slog.Error("LSP server stderr output", "value", buf[:n])
		}

		if err != nil {
			break
		}
	}
}

func (s *Server) Connect(method string) error {
	if s.conn != nil {
		return fmt.Errorf("already connected to server")
	}

	if method == ServerConnectStdio {
		var err error
		s.conn, err = newServerConnPipe(s.cmd)
		if err != nil {
			return err
		}
	}

	if s.cmd != nil {
		err := s.cmd.Start()
		if err != nil {
			return err
		}

		if method == ServerConnectStdio {
			go s.forwardStderr()
		}
	}

	if method != ServerConnectStdio {
		var err error
		s.conn, err = newServerConnTCP(method)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) ShutdownAndExit() error {
	if s.cmd == nil {
		return nil
	}

	client := NewClient(s)
	client.Send(&Message{Method: "shutdown", Id: "shutdown"})
	client.Send(&Message{Method: "exit"})

	return s.cmd.Wait()
}

func (s *Server) read(p []byte) (int, error) {
	return s.conn.read(p)
}

func (s *Server) write(p []byte) (int, error) {
	return s.conn.write(p)
}

func (s *Server) lock() {
	s.mutex.Lock()
}

func (s *Server) unlock() {
	s.mutex.Unlock()
}
