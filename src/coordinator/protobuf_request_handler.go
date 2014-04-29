package coordinator

import (
	"bytes"
	"cluster"
	"common"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"parser"
	"protocol"

	log "code.google.com/p/log4go"
)

type ProtobufRequestHandler struct {
	coordinator   Coordinator
	clusterConfig *cluster.ClusterConfiguration
	writeOk       protocol.Response_Type
}

var (
	internalError        = protocol.Response_INTERNAL_ERROR
	accessDeniedResponse = protocol.Response_ACCESS_DENIED
)

func NewProtobufRequestHandler(coordinator Coordinator, clusterConfig *cluster.ClusterConfiguration) *ProtobufRequestHandler {
	return &ProtobufRequestHandler{coordinator: coordinator, writeOk: protocol.Response_WRITE_OK, clusterConfig: clusterConfig}
}

func (self *ProtobufRequestHandler) HandleRequest(request *protocol.Request, conn net.Conn) error {
	if *request.Type == protocol.Request_WRITE {
		shard := self.clusterConfig.GetLocalShardById(*request.ShardId)
		log.Debug("HANDLE: (%d):%d:%v", self.clusterConfig.LocalServer.Id, request.GetId(), shard)
		err := shard.WriteLocalOnly(request)
		if err != nil {
			log.Error("ProtobufRequestHandler: error writing local shard: ", err)
			return err
		}
		response := &protocol.Response{RequestId: request.Id, Type: &self.writeOk}
		return self.WriteResponse(conn, response)
	} else if *request.Type == protocol.Request_DROP_DATABASE {
		go self.handleDropDatabase(request, conn)
		return nil
	} else if *request.Type == protocol.Request_QUERY {
		go self.handleQuery(request, conn)
	} else if *request.Type == protocol.Request_HEARTBEAT {
		response := &protocol.Response{RequestId: request.Id, Type: &heartbeatResponse}
		return self.WriteResponse(conn, response)
	} else {
		log.Error("unknown request type: %v", request)
		return errors.New("Unknown request type")
	}
	return nil
}

func (self *ProtobufRequestHandler) handleQuery(request *protocol.Request, conn net.Conn) {
	// the query should always parse correctly since it was parsed at the originating server.
	queries, err := parser.ParseQuery(*request.Query)
	if err != nil || len(queries) < 1 {
		log.Error("Error parsing query: ", err)
		errorMsg := fmt.Sprintf("Cannot find user %s", *request.UserName)
		response := &protocol.Response{Type: &endStreamResponse, ErrorMessage: &errorMsg, RequestId: request.Id}
		self.WriteResponse(conn, response)
		return
	}
	query := queries[0]
	var user common.User
	if *request.IsDbUser {
		user = self.clusterConfig.GetDbUser(*request.Database, *request.UserName)
	} else {
		user = self.clusterConfig.GetClusterAdmin(*request.UserName)
	}

	if user == nil {
		errorMsg := fmt.Sprintf("Cannot find user %s", *request.UserName)
		response := &protocol.Response{Type: &accessDeniedResponse, ErrorMessage: &errorMsg, RequestId: request.Id}
		self.WriteResponse(conn, response)
		return
	}

	shard := self.clusterConfig.GetLocalShardById(*request.ShardId)

	querySpec := parser.NewQuerySpec(user, *request.Database, query)

	responseChan := make(chan *protocol.Response)
	if querySpec.IsDestructiveQuery() {
		go shard.HandleDestructiveQuery(querySpec, request, responseChan, true)
	} else {
		go shard.Query(querySpec, responseChan)
	}
	for {
		response := <-responseChan
		response.RequestId = request.Id
		self.WriteResponse(conn, response)
		if response.GetType() == protocol.Response_END_STREAM || response.GetType() == protocol.Response_ACCESS_DENIED {
			return
		}
	}
}

func (self *ProtobufRequestHandler) handleDropDatabase(request *protocol.Request, conn net.Conn) {
	shard := self.clusterConfig.GetLocalShardById(*request.ShardId)
	shard.DropDatabase(*request.Database, false)
	response := &protocol.Response{Type: &endStreamResponse, RequestId: request.Id}
	self.WriteResponse(conn, response)
}

func (self *ProtobufRequestHandler) WriteResponse(conn net.Conn, response *protocol.Response) error {
	data, err := response.Encode()
	if err != nil {
		log.Error("error encoding response: %s", err)
		return err
	}
	if len(data) >= MAX_RESPONSE_SIZE {
		pointCount := len(response.Series.Points)
		firstHalfPoints := response.Series.Points[:pointCount]
		secondHalfPoints := response.Series.Points[pointCount:]
		response.Series.Points = firstHalfPoints
		err := self.WriteResponse(conn, response)
		if err != nil {
			return err
		}
		response.Series.Points = secondHalfPoints
		return self.WriteResponse(conn, response)
	}

	buff := bytes.NewBuffer(make([]byte, 0, len(data)+8))
	binary.Write(buff, binary.LittleEndian, uint32(len(data)))
	_, err = conn.Write(append(buff.Bytes(), data...))
	if err != nil {
		log.Error("error writing response: %s", err)
		return err
	}
	return nil
}
