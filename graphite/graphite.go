package graphite

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdb/influxdb"
)

var (
	// ErrBindAddressRequired is returned when starting the Server
	// without a TCP or UDP listening address.
	ErrBindAddressRequired = errors.New("bind address required")

	// ErrServerClosed return when closing an already closed graphite server.
	ErrServerClosed = errors.New("server already closed")

	// ErrDatabaseNotSpecified retuned when no database was specified in the config file
	ErrDatabaseNotSpecified = errors.New("database was not specified in config")

	// ErrServerNotSpecified returned when Server is not specified.
	ErrServerNotSpecified = errors.New("server not present")
)

// Graphite Server provides a tcp and/or udp listener that you can
// use to ingest metrics into influxdb via the graphite protocol.  it
// behaves as a carbon daemon, except:
//
// no rounding of timestamps to the nearest interval.  Upon ingestion
// of multiple datapoints for a given key within the same interval
// (possibly but not necessarily the same timestamp), graphite would
// use one (the latest received) value with a rounded timestamp
// representing that interval.  We store values for every timestamp we
// receive (only the latest value for a given metric-timestamp pair)
// so it's up to the user to feed the data in proper intervals (and
// use round intervals if you plan to rely on that)
type Server struct {
	mu   sync.Mutex
	wg   sync.WaitGroup
	done chan struct{} // close notification

	Server influxdbServer

	tcpListener *net.TCPListener
	udpConn     *net.UDPConn

	// The name of the database to insert data into.
	Database string
	// Position of name to be parsed from metric_path
	NamePosition string
	// sperator to parse metric_path with to get name and series
	NameSeparator string
}

type influxdbServer interface {
	WriteSeries(database, retentionPolicy, name string, tags map[string]string, timestamp time.Time, values map[string]interface{}) error
	DefaultRetentionPolicy(database string) (*influxdb.RetentionPolicy, error)
}

func NewServer(is influxdbServer) *Server {
	s := Server{Server: is}
	return &s
}

// ListenAndServe opens TCP (and optionally a UDP) socket to listen for messages.
func (s *Server) ListenAndServeTCP(addr *net.TCPAddr) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if addr == nil { // Make sure we have a TCP address
		return ErrBindAddressRequired
	} else if s.Database == "" { // Make sure they have a database
		return ErrDatabaseNotSpecified
	} else if s.Server == nil { // Make sure they specified a server
		return ErrServerNotSpecified
	}

	// Create a new close notification channel.
	done := make(chan struct{}, 0)
	s.done = done

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return err
	}
	s.tcpListener = l

	s.wg.Add(1)
	go s.serveTCP(l, done)

	return nil
}

// serveTCP handles incoming TCP connection requests.
func (s *Server) serveTCP(l *net.TCPListener, done chan struct{}) {
	defer s.wg.Done()

	// Listen for new TCP connections.
	for {
		c, err := l.Accept()
		if err != nil {
			//log.Println("graphite.Server: Accept: ", err)
			return
		}

		s.wg.Add(1)
		go s.handleTCPConn(c)
	}
}

func (s *Server) handleTCPConn(conn net.Conn) {
	defer conn.Close()
	defer s.wg.Done()

	reader := bufio.NewReader(conn)
	for {
		err := s.handleMessage(reader)
		if err != nil {
			if io.EOF == err {
				//log.Println("graphite.Server: Client closed graphite connection")
				return
			}
			log.Println("graphite.Server:", err) // this never shows up in my buffer for testing
		}
	}
}

func (s *Server) ListenAndServeUDP(addr *net.UDPAddr) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if addr == nil { // Make sure we have a TCP address
		return ErrBindAddressRequired
	} else if s.Database == "" { // Make sure they have a database
		return ErrDatabaseNotSpecified
	} else if s.Server == nil { // Make sure they specified a server
		return ErrServerNotSpecified
	}

	// Create a new close notification channel.
	done := make(chan struct{}, 0)
	s.done = done

	//Open the UDP connection.
	c, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	s.udpConn = c

	s.wg.Add(1)
	go s.serveUDP(c, done)

	return nil
}

// serveUDP handles incoming UDP messages.
func (s *Server) serveUDP(conn *net.UDPConn, done chan struct{}) {
	defer s.wg.Done()

	// Listen for server close.
	go func() {
		<-done
		conn.Close()
	}()

	buf := make([]byte, 65536)
	for {
		// Read from connection.
		n, _, err := conn.ReadFromUDP(buf)
		if err == io.EOF {
			return
		} else if err != nil {
			log.Printf("Server: Error when reading from UDP connection %s\n", err.Error())
		}

		// Read in data in a separate goroutine.
		s.wg.Add(1)
		go s.handleUDPMessage(string(buf[:n]))
	}
}

// handleUDPMessage splits a UDP packet by newlines and processes each message.
func (s *Server) handleUDPMessage(msg string) {
	defer s.wg.Done()
	for _, metric := range strings.Split(msg, "\n") {
		s.handleMessage(bufio.NewReader(strings.NewReader(metric + "\n")))
	}
}

// Close shuts down the server's listeners.
func (s *Server) Close() error {
	// Notify other goroutines of shutdown.
	s.mu.Lock()
	if s.done == nil {
		s.mu.Unlock()
		return ErrServerClosed
	}
	close(s.done)
	s.done = nil

	if s.tcpListener != nil {
		_ = s.tcpListener.Close()
	}
	s.tcpListener = nil

	if s.udpConn != nil {
		_ = s.udpConn.Close()
	}
	s.udpConn = nil

	s.mu.Unlock()

	// Wait for all goroutines to shutdown.
	s.wg.Wait()
	return nil
}

// handleMessage decodes a graphite message from the reader and sends it to the
// committer goroutine.
func (s *Server) handleMessage(r *bufio.Reader) error {
	// Decode graphic metric.
	m, err := DecodeMetric(r, s.NamePosition, s.NameSeparator)
	if err != nil {
		return err
	}

	// Convert metric to a field value.
	var values = make(map[string]interface{})
	values[m.Name] = m.Value

	retentionPolicy, err := s.Server.DefaultRetentionPolicy(s.Database)

	if err != nil {
		return fmt.Errorf("error looking up default database retention policy: %s", err)
	}

	if err := s.Server.WriteSeries(
		s.Database,
		retentionPolicy.Name,
		m.Name,
		m.Tags,
		m.Timestamp,
		values,
	); err != nil {
		return fmt.Errorf("write series data: %s", err)
	}
	return nil
}

type Metric struct {
	Name      string
	Tags      map[string]string
	Value     interface{}
	Timestamp time.Time
}

// returns err == io.EOF when we hit EOF without any further data
func DecodeMetric(r *bufio.Reader, position, separator string) (*Metric, error) {
	// Read up to the next newline.
	buf, err := r.ReadBytes('\n')
	if err != nil && err != io.EOF {
		// it's possible to get EOF but also data
		return nil, fmt.Errorf("connection closed uncleanly/broken: %s\n", err.Error())
	}
	// Trim the buffer, even though there should be no padding
	str := strings.TrimSpace(string(buf))
	// Remove line return
	str = strings.TrimSuffix(str, `\n`)
	if str == "" {
		return nil, err
	}
	// Break into 3 fields (name, value, timestamp).
	fields := strings.Fields(str)
	if len(fields) != 3 {
		return nil, fmt.Errorf("received %q which doesn't have three fields", str)
	}

	m := new(Metric)
	// decode the name and tags
	name, tags, err := DecodeNameAndTags(fields[0], position, separator)
	if err != nil {
		return nil, err
	}
	m.Name = name
	m.Tags = tags

	// Parse value.
	v, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, err
	}

	// Determine if value is a float or an int.
	if i := int64(v); float64(i) == v {
		m.Value = int64(v)
	} else {
		m.Value = v
	}

	// Parse timestamp.
	unixTime, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return nil, err
	}

	m.Timestamp = time.Unix(0, unixTime*int64(time.Millisecond))

	return m, nil
}

func DecodeNameAndTags(field, position, separator string) (string, map[string]string, error) {
	var (
		name string
		tags = make(map[string]string)
	)

	if separator == "" {
		separator = "."
	}

	// decode the name and tags
	values := strings.Split(field, separator)
	if len(values)%2 != 1 {
		// There should always be an odd number of fields to map a metric name and tags
		// ex: region.us-west.hostname.server01.cpu -> tags -> region: us-west, hostname: server01, metric name -> cpu
		return name, tags, fmt.Errorf("received %q which doesn't conform to format of key.value.key.value.metric or metric", field)
	}

	np := strings.ToLower(strings.TrimSpace(position))
	switch np {
	case "last":
		name = values[len(values)-1]
		values = values[0 : len(values)-1]
	case "first":
		name = values[0]
		values = values[1:len(values)]
	default:
		name = values[0]
		values = values[1:len(values)]
	}
	if name == "" {
		return name, tags, fmt.Errorf("no name specified for metric. %q", field)
	}

	// Grab the pairs and throw them in the map
	for i := 0; i < len(values); i += 2 {
		k := values[i]
		v := values[i+1]
		tags[k] = v
	}

	return name, tags, nil
}
