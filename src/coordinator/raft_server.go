package coordinator

import (
	"bytes"
	log "code.google.com/p/log4go"
	"common"
	"configuration"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/goraft/raft"
	"github.com/gorilla/mux"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"parser"
	"path/filepath"
	"protocol"
	"strings"
	"sync"
	"time"
)

const (
	DEFAULT_ROOT_PWD = "root"
)

// The raftd server is a combination of the Raft server and an HTTP
// server which acts as the transport.
type RaftServer struct {
	name          string
	host          string
	port          int
	path          string
	bind_address  string
	router        *mux.Router
	raftServer    raft.Server
	httpServer    *http.Server
	clusterConfig *ClusterConfiguration
	mutex         sync.RWMutex
	listener      net.Listener
	closing       bool
	config        *configuration.Configuration
	notLeader     chan bool
	engine        queryRunner
	coordinator   *CoordinatorImpl
}

type queryRunner interface {
	RunQuery(user common.User, database string, query string, localOnly bool, yield func(*protocol.Series) error) error
}

var registeredCommands bool
var replicateWrite = protocol.Request_REPLICATION_WRITE
var replicateDelete = protocol.Request_REPLICATION_DELETE

// Creates a new server.
func NewRaftServer(config *configuration.Configuration, clusterConfig *ClusterConfiguration) *RaftServer {
	if !registeredCommands {
		registeredCommands = true
		for _, command := range internalRaftCommands {
			raft.RegisterCommand(command)
		}
	}

	s := &RaftServer{
		host:          config.HostnameOrDetect(),
		port:          config.RaftServerPort,
		path:          config.RaftDir,
		bind_address:  config.BindAddress,
		clusterConfig: clusterConfig,
		notLeader:     make(chan bool, 1),
		router:        mux.NewRouter(),
		config:        config,
	}
	// Read existing name or generate a new one.
	if b, err := ioutil.ReadFile(filepath.Join(s.path, "name")); err == nil {
		s.name = string(b)
	} else {
		var i uint64
		if _, err := os.Stat("/dev/random"); err == nil {
			log.Info("Using /dev/random to initialize the raft server name")
			f, err := os.Open("/dev/random")
			if err != nil {
				panic(err)
			}
			defer f.Close()
			readBytes := 0
			b := make([]byte, 8)
			for readBytes < 8 {
				n, err := f.Read(b[readBytes:])
				if err != nil {
					panic(err)
				}
				readBytes += n
			}
			err = binary.Read(bytes.NewBuffer(b), binary.BigEndian, &i)
			if err != nil {
				panic(err)
			}
		} else {
			log.Info("Using rand package to generate raft server name")
			rand.Seed(time.Now().UnixNano())
			i = uint64(rand.Int())
		}
		s.name = fmt.Sprintf("%07x", i)[0:7]
		log.Info("Setting raft name to %s", s.name)
		if err = ioutil.WriteFile(filepath.Join(s.path, "name"), []byte(s.name), 0644); err != nil {
			panic(err)
		}
	}

	return s
}

func (s *RaftServer) leaderConnectString() (string, bool) {
	leader := s.raftServer.Leader()
	peers := s.raftServer.Peers()
	if peer, ok := peers[leader]; !ok {
		return "", false
	} else {
		return peer.ConnectionString, true
	}
}

func (s *RaftServer) doOrProxyCommand(command raft.Command, commandType string) (interface{}, error) {
	if s.raftServer.State() == raft.Leader {
		value, err := s.raftServer.Do(command)
		if err != nil {
			log.Error("Cannot run command %#v. %s", command, err)
		}
		return value, err
	} else {
		if leader, ok := s.leaderConnectString(); !ok {
			return nil, errors.New("Couldn't connect to the cluster leader...")
		} else {
			var b bytes.Buffer
			json.NewEncoder(&b).Encode(command)
			resp, err := http.Post(leader+"/process_command/"+commandType, "application/json", &b)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			body, err2 := ioutil.ReadAll(resp.Body)

			if resp.StatusCode != 200 {
				return nil, errors.New(strings.TrimSpace(string(body)))
			}

			var js interface{}
			json.Unmarshal(body, &js)
			return js, err2
		}
	}
	return nil, nil
}

func (s *RaftServer) CreateDatabase(name string, replicationFactor uint8) error {
	if replicationFactor == 0 {
		replicationFactor = 1
	}
	command := NewCreateDatabaseCommand(name, replicationFactor)
	_, err := s.doOrProxyCommand(command, "create_db")
	return err
}

func (s *RaftServer) DropDatabase(name string) error {
	command := NewDropDatabaseCommand(name)
	_, err := s.doOrProxyCommand(command, "drop_db")
	return err
}

func (s *RaftServer) SaveDbUser(u *dbUser) error {
	command := NewSaveDbUserCommand(u)
	_, err := s.doOrProxyCommand(command, "save_db_user")
	return err
}

func (s *RaftServer) ChangeDbUserPassword(db, username string, hash []byte) error {
	command := NewChangeDbUserPasswordCommand(db, username, string(hash))
	_, err := s.doOrProxyCommand(command, "change_db_user_password")
	return err
}

func (s *RaftServer) SaveClusterAdminUser(u *clusterAdmin) error {
	command := NewSaveClusterAdminCommand(u)
	_, err := s.doOrProxyCommand(command, "save_cluster_admin_user")
	return err
}

func (s *RaftServer) CreateRootUser() error {
	u := &clusterAdmin{CommonUser{"root", "", false, "root"}}
	hash, _ := hashPassword(DEFAULT_ROOT_PWD)
	u.changePassword(string(hash))
	return s.SaveClusterAdminUser(u)
}

func (s *RaftServer) SetContinuousQueryTimestamp(timestamp time.Time) error {
	command := NewSetContinuousQueryTimestampCommand(timestamp)
	_, err := s.doOrProxyCommand(command, "set_cq_ts")
	return err
}

func (s *RaftServer) CreateContinuousQuery(db string, query string) error {
	// if there are already-running queries, we need to initiate a backfill
	if !s.clusterConfig.continuousQueryTimestamp.IsZero() {
		selectQuery, err := parser.ParseSelectQuery(query)
		if err != nil {
			return fmt.Errorf("Failed to parse continuous query: %s", query)
		}

		duration, err := selectQuery.GetGroupByClause().GetGroupByTime()
		if err != nil {
			return fmt.Errorf("Couldn't get group by time for continuous query: %s", err)
		}

		if duration != nil {
			zeroTime := time.Time{}
			currentBoundary := time.Now().Truncate(*duration)
			go s.runContinuousQuery(db, selectQuery, zeroTime, currentBoundary)
		}
	}

	command := NewCreateContinuousQueryCommand(db, query)
	_, err := s.doOrProxyCommand(command, "create_cq")
	return err
}

func (s *RaftServer) DeleteContinuousQuery(db string, id uint32) error {
	command := NewDeleteContinuousQueryCommand(db, id)
	_, err := s.doOrProxyCommand(command, "delete_cq")
	return err
}

func (s *RaftServer) ActivateServer(server *ClusterServer) error {
	return errors.New("not implemented")
}

func (s *RaftServer) AddServer(server *ClusterServer, insertIndex int) error {
	return errors.New("not implemented")
}

func (s *RaftServer) MovePotentialServer(server *ClusterServer, insertIndex int) error {
	return errors.New("not implemented")
}

func (s *RaftServer) ReplaceServer(oldServer *ClusterServer, replacement *ClusterServer) error {
	return errors.New("not implemented")
}

func (s *RaftServer) AssignEngineAndCoordinator(engine queryRunner, coordinator *CoordinatorImpl) error {
	s.engine = engine
	s.coordinator = coordinator
	return nil
}

func (s *RaftServer) connectionString() string {
	return fmt.Sprintf("http://%s:%d", s.host, s.port)
}

const (
	MAX_SIZE = 10 * MEGABYTE
)

func (s *RaftServer) forceLogCompaction() {
	err := s.raftServer.TakeSnapshot()
	if err != nil {
		log.Error("Cannot take snapshot: %s", err)
	}
}

func (s *RaftServer) CompactLog() {
	checkSizeTicker := time.Tick(time.Minute)
	forceCompactionTicker := time.Tick(time.Hour * 24)

	for {
		select {
		case <-checkSizeTicker:
			log.Debug("Testing if we should compact the raft logs")

			path := s.raftServer.LogPath()
			size, err := common.GetFileSize(path)
			if err != nil {
				log.Error("Error getting size of file '%s': %s", path, err)
			}
			if size < MAX_SIZE {
				continue
			}
			s.forceLogCompaction()
		case <-forceCompactionTicker:
			s.forceLogCompaction()
		}
	}
}

func (s *RaftServer) startRaft() error {
	log.Info("Initializing Raft Server: %s %d", s.path, s.port)

	// Initialize and start Raft server.
	transporter := raft.NewHTTPTransporter("/raft")
	var err error
	s.raftServer, err = raft.NewServer(s.name, s.path, transporter, s.clusterConfig, s.clusterConfig, "")
	if err != nil {
		return err
	}

	s.raftServer.LoadSnapshot() // ignore errors

	s.raftServer.AddEventListener(raft.StateChangeEventType, s.raftEventHandler)

	transporter.Install(s.raftServer, s)
	s.raftServer.Start()

	go s.CompactLog()

	if !s.raftServer.IsLogEmpty() {
		log.Info("Recovered from log")
		return nil
	}

	potentialLeaders := s.config.SeedServers

	if len(potentialLeaders) == 0 {
		log.Info("Starting as new Raft leader...")
		name := s.raftServer.Name()
		connectionString := s.connectionString()
		_, err := s.raftServer.Do(&InfluxJoinCommand{
			Name:                     name,
			ConnectionString:         connectionString,
			ProtobufConnectionString: s.config.ProtobufConnectionString(),
		})

		if err != nil {
			log.Error(err)
		}

		command := NewAddPotentialServerCommand(&ClusterServer{
			RaftName:                 name,
			RaftConnectionString:     connectionString,
			ProtobufConnectionString: s.config.ProtobufConnectionString(),
		})
		_, err = s.doOrProxyCommand(command, "add_server")
		if err != nil {
			return err
		}
		err = s.CreateRootUser()
		return err
	}

	for {
		for _, leader := range potentialLeaders {
			log.Info("(raft:%s) Attempting to join leader: %s", s.raftServer.Name(), leader)

			if err := s.Join(leader); err == nil {
				log.Info("Joined: %s", leader)
				return nil
			}
		}

		log.Warn("Couldn't join any of the seeds, sleeping and retrying...")
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

func (s *RaftServer) raftEventHandler(e raft.Event) {
	if e.Value() == "leader" {
		log.Info("(raft:%s) Selected as leader. Starting leader loop.", s.raftServer.Name())
		go s.raftLeaderLoop(time.NewTicker(1 * time.Second))
	}

	if e.PrevValue() == "leader" {
		log.Info("(raft:%s) Demoted from leader. Ending leader loop.", s.raftServer.Name())
		s.notLeader <- true
	}
}

func (s *RaftServer) raftLeaderLoop(loopTimer *time.Ticker) {
	for {
		select {
		case <-loopTimer.C:
			log.Debug("(raft:%s) Executing leader loop.", s.raftServer.Name())
			s.checkContinuousQueries()
			break
		case <-s.notLeader:
			log.Debug("(raft:%s) Exiting leader loop.", s.raftServer.Name())
			return
		}
	}
}

func (s *RaftServer) checkContinuousQueries() {
	if s.clusterConfig.continuousQueries == nil || len(s.clusterConfig.continuousQueries) == 0 {
		return
	}

	runTime := time.Now()
	queriesDidRun := false

	for db, queries := range s.clusterConfig.parsedContinuousQueries {
		for _, query := range queries {
			groupByClause := query.GetGroupByClause()

			// if there's no group by clause, it's handled as a fanout query
			if groupByClause.Elems == nil {
				continue
			}

			duration, err := query.GetGroupByClause().GetGroupByTime()
			if err != nil {
				log.Error("Couldn't get group by time for continuous query:", err)
				continue
			}

			currentBoundary := runTime.Truncate(*duration)
			lastBoundary := s.clusterConfig.continuousQueryTimestamp.Truncate(*duration)

			if currentBoundary.After(s.clusterConfig.continuousQueryTimestamp) {
				s.runContinuousQuery(db, query, lastBoundary, currentBoundary)
				queriesDidRun = true
			}
		}
	}

	if queriesDidRun {
		s.clusterConfig.continuousQueryTimestamp = runTime
		s.SetContinuousQueryTimestamp(runTime)
	}
}

func (s *RaftServer) runContinuousQuery(db string, query *parser.SelectQuery, start time.Time, end time.Time) {
	clusterAdmin := s.clusterConfig.clusterAdmins["root"]
	intoClause := query.GetIntoClause()
	targetName := intoClause.Target.Name
	sequenceNumber := uint64(1)
	queryString := query.GetQueryStringForContinuousQuery(start, end)

	s.engine.RunQuery(clusterAdmin, db, queryString, false, func(series *protocol.Series) error {
		interpolatedTargetName := strings.Replace(targetName, ":series_name", *series.Name, -1)
		series.Name = &interpolatedTargetName
		for _, point := range series.Points {
			point.SequenceNumber = &sequenceNumber
		}

		return s.coordinator.WriteSeriesData(clusterAdmin, db, series)
	})
}

func (s *RaftServer) ListenAndServe() error {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.bind_address, s.port))
	if err != nil {
		panic(err)
	}
	return s.Serve(l)
}

func (s *RaftServer) Serve(l net.Listener) error {
	s.port = l.Addr().(*net.TCPAddr).Port
	s.listener = l

	log.Info("Initializing Raft HTTP server")

	// Initialize and start HTTP server.
	s.httpServer = &http.Server{
		Handler: s.router,
	}

	s.router.HandleFunc("/cluster_config", s.configHandler).Methods("GET")
	s.router.HandleFunc("/join", s.joinHandler).Methods("POST")
	s.router.HandleFunc("/process_command/{command_type}", s.processCommandHandler).Methods("POST")

	log.Info("Raft Server Listening at %s", s.connectionString())

	go func() {
		err := s.httpServer.Serve(l)
		if !strings.Contains(err.Error(), "closed network") {
			panic(err)
		}
	}()
	started := make(chan error)
	go func() {
		started <- s.startRaft()
	}()
	err := <-started
	//	time.Sleep(3 * time.Second)
	return err
}

func (self *RaftServer) Close() {
	if !self.closing || self.raftServer == nil {
		self.closing = true
		self.raftServer.Stop()
		self.listener.Close()
		self.notLeader <- true
	}
}

// This is a hack around Gorilla mux not providing the correct net/http
// HandleFunc() interface.
func (s *RaftServer) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	s.router.HandleFunc(pattern, handler)
}

// Joins to the leader of an existing cluster.
func (s *RaftServer) Join(leader string) error {
	command := &InfluxJoinCommand{
		Name:                     s.raftServer.Name(),
		ConnectionString:         s.connectionString(),
		ProtobufConnectionString: s.config.ProtobufConnectionString(),
	}
	connectUrl := leader
	if !strings.HasPrefix(connectUrl, "http://") {
		connectUrl = "http://" + connectUrl
	}
	if !strings.HasSuffix(connectUrl, "/join") {
		connectUrl = connectUrl + "/join"
	}

	var b bytes.Buffer
	json.NewEncoder(&b).Encode(command)
	log.Debug("(raft:%s) Posting to seed server %s", s.raftServer.Name(), connectUrl)
	tr := &http.Transport{
		ResponseHeaderTimeout: time.Second,
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Post(connectUrl, "application/json", &b)
	if err != nil {
		log.Error(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTemporaryRedirect {
		address := resp.Header.Get("Location")
		log.Debug("Redirected to %s to join leader\n", address)
		return s.Join(address)
	}

	log.Debug("(raft:%s) Posted to seed server %s", s.raftServer.Name(), connectUrl)
	return nil
}

func (s *RaftServer) retryCommand(command raft.Command, retries int) (ret interface{}, err error) {
	for retries = retries; retries > 0; retries-- {
		ret, err = s.raftServer.Do(command)
		if err == nil {
			return ret, nil
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Println("Retrying RAFT command...")
	}
	return
}

func (s *RaftServer) joinHandler(w http.ResponseWriter, req *http.Request) {
	if s.raftServer.State() == raft.Leader {
		command := &InfluxJoinCommand{}
		if err := json.NewDecoder(req.Body).Decode(&command); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Debug("Leader processing: %v", command)
		// during the test suite the join command will sometimes time out.. just retry a few times
		if _, err := s.raftServer.Do(command); err != nil {
			log.Error("Can't process %v: %s", command, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		server := s.clusterConfig.GetServerByRaftName(command.Name)
		// it's a new server the cluster has never seen, make it a potential
		if server == nil {
			log.Info("Adding new server to the cluster config %s", command.Name)
			addServer := NewAddPotentialServerCommand(&ClusterServer{
				RaftName:                 command.Name,
				RaftConnectionString:     command.ConnectionString,
				ProtobufConnectionString: command.ProtobufConnectionString,
			})
			if _, err := s.raftServer.Do(addServer); err != nil {
				log.Error("Error joining raft server: ", err, command)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		log.Info("Server %s already exist in the cluster config", command.Name)
	} else {
		leader, ok := s.leaderConnectString()
		log.Debug("Non-leader redirecting to: (%v, %v)", leader, ok)
		if ok {
			log.Debug("redirecting to leader to join...")
			http.Redirect(w, req, leader+"/join", http.StatusTemporaryRedirect)
		} else {
			http.Error(w, errors.New("Couldn't find leader of the cluster to join").Error(), http.StatusInternalServerError)
		}
	}
}

func (s *RaftServer) configHandler(w http.ResponseWriter, req *http.Request) {
	jsonObject := make(map[string]interface{})
	dbs := make([]string, 0)
	for db, _ := range s.clusterConfig.databaseReplicationFactors {
		dbs = append(dbs, db)
	}
	jsonObject["databases"] = dbs
	jsonObject["cluster_admins"] = s.clusterConfig.clusterAdmins
	jsonObject["database_users"] = s.clusterConfig.dbUsers
	js, err := json.Marshal(jsonObject)
	if err != nil {
		log.Error("ERROR marshalling config: ", err)
	}
	w.Write(js)
}

func (s *RaftServer) marshalAndDoCommandFromBody(command raft.Command, req *http.Request) (interface{}, error) {
	if err := json.NewDecoder(req.Body).Decode(&command); err != nil {
		return nil, err
	}
	if result, err := s.raftServer.Do(command); err != nil {
		return nil, err
	} else {
		return result, nil
	}
}

func (s *RaftServer) processCommandHandler(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	value := vars["command_type"]
	command := internalRaftCommands[value]

	if result, err := s.marshalAndDoCommandFromBody(command, req); err != nil {
		log.Error("command %T failed: %s", command, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	} else {
		if result != nil {
			js, _ := json.Marshal(result)
			w.Write(js)
		}
	}
}
