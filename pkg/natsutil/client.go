package natsutil

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type Client struct {
	conn   net.Conn
	rw     *bufio.ReadWriter
	mu     sync.Mutex
	subs   map[int]Handler
	sid    int
	closed chan struct{}
}

func Connect(rawURL string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if host == "" {
		host = "127.0.0.1:4222"
	}
	conn, err := net.Dial("tcp", host)
	if err != nil {
		return nil, err
	}
	client := &Client{
		conn:   conn,
		rw:     bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
		subs:   make(map[int]Handler),
		closed: make(chan struct{}),
	}
	if err := client.initialize(); err != nil {
		conn.Close()
		return nil, err
	}
	go client.readLoop()
	return client, nil
}

func (c *Client) initialize() error {
	info, err := c.rw.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(info, "INFO") {
		return fmt.Errorf("unexpected server greeting: %s", strings.TrimSpace(info))
	}
	if _, err := c.rw.WriteString("CONNECT {\"verbose\":false,\"pedantic\":false}\r\n"); err != nil {
		return err
	}
	if err := c.rw.Flush(); err != nil {
		return err
	}
	return nil
}

func (c *Client) readLoop() {
	for {
		line, err := c.rw.ReadString('\n')
		if err != nil {
			close(c.closed)
			return
		}
		line = strings.TrimSpace(line)
		if line == "PING" {
			c.mu.Lock()
			c.rw.WriteString("PONG\r\n")
			c.rw.Flush()
			c.mu.Unlock()
			continue
		}
		if strings.HasPrefix(line, "MSG") {
			parts := strings.Split(line, " ")
			if len(parts) < 4 {
				continue
			}
			sid, _ := strconv.Atoi(parts[2])
			size, _ := strconv.Atoi(parts[len(parts)-1])
			payload := make([]byte, size+2)
			if _, err := io.ReadFull(c.rw, payload); err != nil {
				continue
			}
			data := payload[:size]
			handler := c.getHandler(sid)
			if handler != nil {
				copyData := append([]byte(nil), data...)
				go handler(copyData)
			}
		}
	}
}

func (c *Client) getHandler(sid int) Handler {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subs[sid]
}

func (c *Client) Publish(subject string, msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.rw.WriteString(fmt.Sprintf("PUB %s %d\r\n", subject, len(msg))); err != nil {
		return err
	}
	if _, err := c.rw.Write(msg); err != nil {
		return err
	}
	if _, err := c.rw.WriteString("\r\n"); err != nil {
		return err
	}
	return c.rw.Flush()
}

func (c *Client) Subscribe(subject string, handler Handler) (Subscription, error) {
	if handler == nil {
		return nil, fmt.Errorf("handler is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sid++
	sid := c.sid
	c.subs[sid] = handler
	if _, err := c.rw.WriteString(fmt.Sprintf("SUB %s %d\r\n", subject, sid)); err != nil {
		delete(c.subs, sid)
		return nil, err
	}
	if err := c.rw.Flush(); err != nil {
		delete(c.subs, sid)
		return nil, err
	}
	return &clientSub{client: c, sid: sid}, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

type clientSub struct {
	client *Client
	sid    int
}

func (s *clientSub) Unsubscribe() error {
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	delete(s.client.subs, s.sid)
	if s.client.conn == nil {
		return nil
	}
	s.client.rw.WriteString(fmt.Sprintf("UNSUB %d\r\n", s.sid))
	return s.client.rw.Flush()
}
