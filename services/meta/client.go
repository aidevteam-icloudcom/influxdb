package meta

import (
	"bytes"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/influxdb/influxdb"
	"github.com/influxdb/influxdb/influxql"
	"github.com/influxdb/influxdb/services/meta/internal"

	"github.com/gogo/protobuf/proto"
)

const (
	// errSleep is the time to sleep after we've failed on every metaserver
	// before making another pass
	errSleep = time.Second

	// maxRetries is the maximum number of attemps to make before returning
	// a failure to the caller
	maxRetries = 10

	// SaltBytes is the number of bytes used for salts
	SaltBytes = 32
)

// Client is used to execute commands on and read data from
// a meta service cluster.
type Client struct {
	tls    bool
	logger *log.Logger

	mu          sync.RWMutex
	metaServers []string
	changed     chan struct{}
	closing     chan struct{}
	data        *Data

	executor *StatementExecutor

	// Authentication cache.
	authCache map[string]authUser

	// hashPassword generates a cryptographically secure hash for password.
	// Returns an error if the password is invalid or a hash cannot be generated.
	hashPassword HashPasswordFn
}

// NewClient returns a new *Client.
func NewClient(metaServers []string, tls bool) *Client {
	client := &Client{
		data:        &Data{},
		metaServers: metaServers,
		tls:         tls,
		logger:      log.New(os.Stderr, "[metaclient] ", log.LstdFlags),
		authCache:   make(map[string]authUser, 0),
		hashPassword: func(password string) ([]byte, error) {
			return bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
		},
	}
	client.executor = &StatementExecutor{Store: client}
	return client
}

// Open a connection to a meta service cluster.
func (c *Client) Open() error {
	c.changed = make(chan struct{})
	c.closing = make(chan struct{})
	c.data = c.retryUntilSnapshot(0)

	go c.pollForUpdates()

	return nil
}

// Close the meta service cluster connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.closing:
		return nil
	default:
		close(c.closing)
	}

	return nil
}

// ClusterID returns the ID of the cluster it's connected to.
func (c *Client) ClusterID() (id uint64, err error) {
	return 0, nil
}

// Node returns a node by id.
func (c *Client) DataNode(id uint64) (*NodeInfo, error) {
	return nil, nil
}

// DataNodes returns the data nodes' info.
func (c *Client) DataNodes() ([]NodeInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data.DataNodes, nil
}

// CreateDataNode will create a new data node in the metastore
func (c *Client) CreateDataNode(httpAddr, tcpAddr string) (*NodeInfo, error) {
	cmd := &internal.CreateDataNodeCommand{
		HTTPAddr: proto.String(httpAddr),
		TCPAddr:  proto.String(tcpAddr),
	}

	if err := c.retryUntilExec(internal.Command_CreateDataNodeCommand, internal.E_CreateDataNodeCommand_Command, cmd); err != nil {
		return nil, err
	}

	return c.DataNodeByHTTPHost(httpAddr)
}

// DataNodeByHTTPHost returns the data node with the give http bind address
func (c *Client) DataNodeByHTTPHost(httpAddr string) (*NodeInfo, error) {
	nodes, _ := c.DataNodes()
	for _, n := range nodes {
		if n.Host == httpAddr {
			return &n, nil
		}
	}

	return nil, ErrNodeNotFound
}

// DeleteDataNode deletes a data node from the cluster.
func (c *Client) DeleteDataNode(nodeID uint64) error {
	return nil
}

// MetaNodes returns the meta nodes' info.
func (c *Client) MetaNodes() ([]NodeInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data.MetaNodes, nil
}

// MetaNodeByAddr returns the meta node's info.
func (c *Client) MetaNodeByAddr(addr string) *NodeInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, n := range c.data.MetaNodes {
		if n.Host == addr {
			return &n
		}
	}
	return nil
}

// Database returns info for the requested database.
func (c *Client) Database(name string) (*DatabaseInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, d := range c.data.Databases {
		if d.Name == name {
			return &d, nil
		}
	}

	return nil, influxdb.ErrDatabaseNotFound(name)
}

// Databases returns a list of all database infos.
func (c *Client) Databases() ([]DatabaseInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.data.Databases == nil {
		return []DatabaseInfo{}, nil
	}
	return c.data.Databases, nil
}

// CreateDatabase creates a database.
func (c *Client) CreateDatabase(name string) (*DatabaseInfo, error) {
	cmd := &internal.CreateDatabaseCommand{
		Name: proto.String(name),
	}

	err := c.retryUntilExec(internal.Command_CreateDatabaseCommand, internal.E_CreateDatabaseCommand_Command, cmd)
	if err != nil {
		return nil, err
	}

	return c.Database(name)
}

// CreateDatabaseWithRetentionPolicy creates a database with the specified retention policy.
func (c *Client) CreateDatabaseWithRetentionPolicy(name string, rpi *RetentionPolicyInfo) (*DatabaseInfo, error) {
	if rpi.Duration < MinRetentionPolicyDuration && rpi.Duration != 0 {
		return nil, ErrRetentionPolicyDurationTooLow
	}

	if _, err := c.CreateDatabase(name); err != nil {
		return nil, err
	}

	cmd := &internal.CreateRetentionPolicyCommand{
		Database:        proto.String(name),
		RetentionPolicy: rpi.marshal(),
	}

	if err := c.retryUntilExec(internal.Command_CreateRetentionPolicyCommand, internal.E_CreateRetentionPolicyCommand_Command, cmd); err != nil {
		return nil, err
	}

	return c.Database(name)
}

// DropDatabase deletes a database.
func (c *Client) DropDatabase(name string) error {
	cmd := &internal.DropDatabaseCommand{
		Name: proto.String(name),
	}

	return c.retryUntilExec(internal.Command_DropDatabaseCommand, internal.E_DropDatabaseCommand_Command, cmd)
}

// CreateRetentionPolicy creates a retention policy on the specified database.
func (c *Client) CreateRetentionPolicy(database string, rpi *RetentionPolicyInfo) (*RetentionPolicyInfo, error) {
	if rpi.Duration < MinRetentionPolicyDuration && rpi.Duration != 0 {
		return nil, ErrRetentionPolicyDurationTooLow
	}

	cmd := &internal.CreateRetentionPolicyCommand{
		Database:        proto.String(database),
		RetentionPolicy: rpi.marshal(),
	}

	if err := c.retryUntilExec(internal.Command_CreateRetentionPolicyCommand, internal.E_CreateRetentionPolicyCommand_Command, cmd); err != nil {
		return nil, err
	}

	return c.RetentionPolicy(database, rpi.Name)
}

// RetentionPolicy returns the requested retention policy info.
func (c *Client) RetentionPolicy(database, name string) (rpi *RetentionPolicyInfo, err error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	db, err := c.Database(database)
	if err != nil {
		return nil, err
	}

	return db.RetentionPolicy(name), nil
}

// VisitRetentionPolicies executes the given function on all retention policies in all databases.
func (c *Client) VisitRetentionPolicies(f func(d DatabaseInfo, r RetentionPolicyInfo)) {

}

// DropRetentionPolicy drops a retention policy from a database.
func (c *Client) DropRetentionPolicy(database, name string) error {
	cmd := &internal.DropRetentionPolicyCommand{
		Database: proto.String(database),
		Name:     proto.String(name),
	}

	return c.retryUntilExec(internal.Command_DropRetentionPolicyCommand, internal.E_DropRetentionPolicyCommand_Command, cmd)
}

// SetDefaultRetentionPolicy sets a database's default retention policy.
func (c *Client) SetDefaultRetentionPolicy(database, name string) error {
	cmd := &internal.SetDefaultRetentionPolicyCommand{
		Database: proto.String(database),
		Name:     proto.String(name),
	}

	return c.retryUntilExec(internal.Command_SetDefaultRetentionPolicyCommand, internal.E_SetDefaultRetentionPolicyCommand_Command, cmd)
}

// UpdateRetentionPolicy updates a retention policy.
func (c *Client) UpdateRetentionPolicy(database, name string, rpu *RetentionPolicyUpdate) error {
	return nil
}

// IsLeader - should get rid of this
func (c *Client) IsLeader() bool {
	return false
}

// WaitForLeader - should get rid of this
func (c *Client) WaitForLeader(timeout time.Duration) error {
	return nil
}

func (c *Client) Users() []UserInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data.Users == nil {
		return []UserInfo{}
	}
	return c.data.Users
}

func (c *Client) User(name string) (*UserInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, u := range c.data.Users {
		if u.Name == name {
			return &u, nil
		}
	}

	return nil, ErrUserNotFound
}

// BcryptCost is the cost associated with generating password with Bcrypt.
// This setting is lowered during testing to improve test suite performance.
var BcryptCost = 10

// HashPasswordFn represnets a password hashing function.
type HashPasswordFn func(password string) ([]byte, error)

// GetHashPasswordFn returns the current password hashing function.
func (c *Client) GetHashPasswordFn() HashPasswordFn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hashPassword
}

// SetHashPasswordFn sets the password hashing function.
func (c *Client) SetHashPasswordFn(fn HashPasswordFn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hashPassword = fn
}

// hashWithSalt returns a salted hash of password using salt
func (c *Client) hashWithSalt(salt []byte, password string) ([]byte, error) {
	hasher := sha256.New()
	hasher.Write(append(salt, []byte(password)...))
	return hasher.Sum(nil), nil
}

// saltedHash returns a salt and salted hash of password
func (c *Client) saltedHash(password string) (salt, hash []byte, err error) {
	salt = make([]byte, SaltBytes)
	_, err = io.ReadFull(crand.Reader, salt)
	if err != nil {
		return
	}

	hash, err = c.hashWithSalt(salt, password)
	return
}

func (c *Client) CreateUser(name, password string, admin bool) (*UserInfo, error) {
	// Hash the password before serializing it.
	hash, err := c.hashPassword(password)
	if err != nil {
		return nil, err
	}

	if err := c.retryUntilExec(internal.Command_CreateUserCommand, internal.E_CreateUserCommand_Command,
		&internal.CreateUserCommand{
			Name:  proto.String(name),
			Hash:  proto.String(string(hash)),
			Admin: proto.Bool(admin),
		},
	); err != nil {
		return nil, err
	}
	return c.User(name)
}

func (c *Client) UpdateUser(name, password string) error {
	// Hash the password before serializing it.
	hash, err := c.hashPassword(password)
	if err != nil {
		return err
	}

	return c.retryUntilExec(internal.Command_UpdateUserCommand, internal.E_UpdateUserCommand_Command,
		&internal.UpdateUserCommand{
			Name: proto.String(name),
			Hash: proto.String(string(hash)),
		},
	)
}

func (c *Client) DropUser(name string) error {
	return c.retryUntilExec(internal.Command_DropUserCommand, internal.E_DropUserCommand_Command,
		&internal.DropUserCommand{
			Name: proto.String(name),
		},
	)
}

func (c *Client) SetPrivilege(username, database string, p influxql.Privilege) error {
	return c.retryUntilExec(internal.Command_SetPrivilegeCommand, internal.E_SetPrivilegeCommand_Command,
		&internal.SetPrivilegeCommand{
			Username:  proto.String(username),
			Database:  proto.String(database),
			Privilege: proto.Int32(int32(p)),
		},
	)
}

func (c *Client) SetAdminPrivilege(username string, admin bool) error {
	return c.retryUntilExec(internal.Command_SetAdminPrivilegeCommand, internal.E_SetAdminPrivilegeCommand_Command,
		&internal.SetAdminPrivilegeCommand{
			Username: proto.String(username),
			Admin:    proto.Bool(admin),
		},
	)
}

func (c *Client) UserPrivileges(username string) (map[string]influxql.Privilege, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p, err := c.data.UserPrivileges(username)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (c *Client) UserPrivilege(username, database string) (*influxql.Privilege, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p, err := c.data.UserPrivilege(username, database)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (c *Client) AdminUserExists() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, u := range c.data.Users {
		if u.Admin {
			return true
		}
	}
	return false
}

func (c *Client) Authenticate(username, password string) (*UserInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find user.
	u := c.data.User(username)
	if u == nil {
		return nil, ErrUserNotFound
	}

	// Check the local auth cache first.
	if au, ok := c.authCache[username]; ok {
		// verify the password using the cached salt and hash
		hashed, err := c.hashWithSalt(au.salt, password)
		if err != nil {
			return nil, err
		}

		if bytes.Equal(hashed, au.hash) {
			return u, nil
		}
		return nil, ErrAuthenticate
	}

	// Compare password with user hash.
	if err := bcrypt.CompareHashAndPassword([]byte(u.Hash), []byte(password)); err != nil {
		return nil, ErrAuthenticate
	}

	// generate a salt and hash of the password for the cache
	salt, hashed, err := c.saltedHash(password)
	if err != nil {
		return nil, err
	}
	c.authCache[username] = authUser{salt: salt, hash: hashed}

	return u, nil
}

func (c *Client) UserCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data.Users)
}

func (c *Client) ShardGroupsByTimeRange(database, policy string, min, max time.Time) (a []ShardGroupInfo, err error) {
	return nil, nil
}

func (c *Client) CreateShardGroupIfNotExists(database, policy string, timestamp time.Time) (*ShardGroupInfo, error) {
	return nil, nil
}

func (c *Client) DeleteShardGroup(database, policy string, id uint64) error {
	return nil
}

func (c *Client) PrecreateShardGroups(from, to time.Time) error {
	return nil
}

func (c *Client) ShardOwner(shardID uint64) (string, string, *ShardGroupInfo) {
	return "", "", nil
}

// JoinMetaServer will add the passed in tcpAddr to the raft peers and add a MetaNode to
// the metastore
func (c *Client) JoinMetaServer(httpAddr, tcpAddr string) error {
	node := &NodeInfo{
		Host:    httpAddr,
		TCPHost: tcpAddr,
	}
	b, err := json.Marshal(node)
	if err != nil {
		return err
	}

	currentServer := 0
	redirectServer := ""
	for {
		// get the server to try to join against
		var url string
		if redirectServer != "" {
			url = redirectServer
			redirectServer = ""
		} else {
			c.mu.RLock()

			if currentServer >= len(c.metaServers) {
				currentServer = 0
			}
			server := c.metaServers[currentServer]
			c.mu.RUnlock()

			url = c.url(server) + "/join"
		}

		resp, err := http.Post(url, "application/json", bytes.NewBuffer(b))
		if err != nil {
			currentServer++
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTemporaryRedirect {
			redirectServer = resp.Header.Get("Location")
			continue
		}

		return nil
	}
}

func (c *Client) CreateMetaNode(httpAddr, tcpAddr string) error {
	cmd := &internal.CreateMetaNodeCommand{
		HTTPAddr: proto.String(httpAddr),
		TCPAddr:  proto.String(tcpAddr),
	}

	return c.retryUntilExec(internal.Command_CreateMetaNodeCommand, internal.E_CreateMetaNodeCommand_Command, cmd)
}

func (c *Client) DeleteMetaNode(id uint64) error {
	cmd := &internal.DeleteMetaNodeCommand{
		ID: proto.Uint64(id),
	}

	return c.retryUntilExec(internal.Command_DeleteMetaNodeCommand, internal.E_DeleteMetaNodeCommand_Command, cmd)
}

func (c *Client) CreateContinuousQuery(database, name, query string) error {
	return nil
}

func (c *Client) DropContinuousQuery(database, name string) error {
	return nil
}

func (c *Client) CreateSubscription(database, rp, name, mode string, destinations []string) error {
	return nil
}

func (c *Client) DropSubscription(database, rp, name string) error {
	return nil
}

func (c *Client) ExecuteStatement(stmt influxql.Statement) *influxql.Result {
	return c.executor.ExecuteStatement(stmt)
}

// WaitForDataChanged will return a channel that will get closed when
// the metastore data has changed
func (c *Client) WaitForDataChanged() chan struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.changed
}

func (c *Client) MarshalBinary() ([]byte, error) {
	return nil, nil
}

func (c *Client) index() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data.Index
}

// retryUntilExec will attempt the command on each of the metaservers until it either succeeds or
// hits the max number of tries
func (c *Client) retryUntilExec(typ internal.Command_Type, desc *proto.ExtensionDesc, value interface{}) error {
	var err error
	var index uint64
	tries := 0
	currentServer := 0
	var redirectServer string

	for {
		c.mu.RLock()
		// exit if we're closed
		select {
		case <-c.closing:
			c.mu.RUnlock()
			return nil
		default:
			// we're still open, continue on
		}
		c.mu.RUnlock()

		// build the url to hit the redirect server or the next metaserver
		var url string
		if redirectServer != "" {
			url = redirectServer
			redirectServer = ""
		} else {
			c.mu.RLock()
			if currentServer >= len(c.metaServers) {
				currentServer = 0
			}
			server := c.metaServers[currentServer]
			c.mu.RUnlock()

			url = fmt.Sprintf("://%s/execute", server)
			if c.tls {
				url = "https" + url
			} else {
				url = "http" + url
			}
		}

		index, err = c.exec(url, typ, desc, value)
		tries++
		currentServer++

		if err == nil {
			c.waitForIndex(index)
			return nil
		}

		if tries > maxRetries {
			return err
		}

		if e, ok := err.(errRedirect); ok {
			redirectServer = e.host
			continue
		}

		time.Sleep(errSleep)
	}
}

func (c *Client) exec(url string, typ internal.Command_Type, desc *proto.ExtensionDesc, value interface{}) (index uint64, err error) {
	// Create command.
	cmd := &internal.Command{Type: &typ}
	if err := proto.SetExtension(cmd, desc, value); err != nil {
		panic(err)
	}

	b, err := proto.Marshal(cmd)
	if err != nil {
		return 0, err
	}

	resp, err := http.Post(url, "application/octet-stream", bytes.NewBuffer(b))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// read the response
	if resp.StatusCode == http.StatusTemporaryRedirect {
		return 0, errRedirect{host: resp.Header.Get("Location")}
	} else if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected result:\n\texp: %d\n\tgot: %d\n", http.StatusOK, resp.StatusCode)
	}

	res := &internal.Response{}

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if err := proto.Unmarshal(b, res); err != nil {
		return 0, err
	}
	es := res.GetError()
	if es != "" {
		return 0, fmt.Errorf("exec err: %s", es)
	}

	return res.GetIndex(), nil
}

func (c *Client) waitForIndex(idx uint64) {
	for {
		c.mu.RLock()
		if c.data.Index >= idx {
			c.mu.RUnlock()
			return
		}
		ch := c.changed
		c.mu.RUnlock()
		<-ch
	}
}

func (c *Client) pollForUpdates() {
	for {
		data := c.retryUntilSnapshot(c.index())
		if data == nil {
			// this will only be nil if the client has been closed,
			// so we can exit out
			return
		}

		// update the data and notify of the change
		c.mu.Lock()
		idx := c.data.Index
		c.data = data
		if idx < data.Index {
			close(c.changed)
			c.changed = make(chan struct{})
		}
		c.mu.Unlock()
	}
}

func (c *Client) getSnapshot(server string, index uint64) (*Data, error) {
	resp, err := http.Get(c.url(server) + fmt.Sprintf("?index=%d", index))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	data := &Data{}
	if err := data.UnmarshalBinary(b); err != nil {
		return nil, err
	}

	return data, nil
}

func (c *Client) url(server string) string {
	url := fmt.Sprintf("://%s", server)

	if c.tls {
		url = "https" + url
	} else {
		url = "http" + url
	}

	return url
}

func (c *Client) retryUntilSnapshot(idx uint64) *Data {
	currentServer := 0
	for {
		// get the index to look from and the server to poll
		c.mu.RLock()

		// exit if we're closed
		select {
		case <-c.closing:
			c.mu.RUnlock()
			return nil
		default:
			// we're still open, continue on
		}

		if currentServer >= len(c.metaServers) {
			currentServer = 0
		}
		server := c.metaServers[currentServer]
		c.mu.RUnlock()

		data, err := c.getSnapshot(server, idx)

		if err == nil {
			return data
		}

		c.logger.Printf("failure getting snapshot from %s: %s", server, err.Error())
		time.Sleep(errSleep)

		currentServer++
	}
}

type errRedirect struct {
	host string
}

func (e errRedirect) Error() string {
	return fmt.Sprintf("redirect to %s", e.host)
}
