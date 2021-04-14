package server

import (
	"bytes"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gwuhaolin/livego/protocol/amf"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"fastlive/pkg/rtmp/chunk"
	"fastlive/pkg/rtmp/connection"
	"fastlive/pkg/rtmp/handshake"
)

type conn struct {
	server *Server

	connection.Connection

	handshakeMutex  sync.Mutex
	handshakeStatus uint32
	handshakeErr    error

	clientConnectInfo       //客户端connect消息
	clientPublishOrPlayInfo //客户端publish消息
	onMetaData              //客户端onMetaData数据

	sess *session
}

func (c *conn) serve() {
	defer c.Connection.Close()
	if err := c.handshake(); err != nil {
		c.server.logger.Error("handshake", zap.Error(err))
		return
	}
	c.server.logger.Debug("handshake success.")

	if err := c.recvChunkStream(); err != nil {
		if errors.Cause(err) != io.EOF {
			c.server.logger.Error("recv Chunk stream", zap.Error(err))
		} else {
			c.server.logger.Debug("serve done", zap.String("client", c.Connection.Rwc.RemoteAddr().String()))
		}
	}
}

func (c *conn) recvChunkStream() error {
	startTime := time.Now()

	for {
		msg, err := c.Connection.RecvIntegralMessage() //TODO: 超时控制，配置net.Conn读超时?
		if err != nil {
			return errors.Wrap(err, "recv intergral message")
		}

		if err := c.onRecvIntegralMessage(msg); err != nil {
			return errors.Wrap(err, "on recv intergral message") //TODO: 超时控制，配置net.Conn写超时?
		}

		switch c.clientPublishOrPlayInfo.clientType {
		case 0:
			if time.Since(startTime) > c.server.config.PlayorPublishTimeout {
				return errors.Errorf("recv publish/play command message timeout")
			}
		case 1: // publish
			if err := c.publishCycle(); err != nil {
				return errors.Wrap(err, "do publish cycle")
			}
		case 2: // play
			if err := c.playCycle(); err != nil {
				return errors.Wrap(err, "do play cycle")
			}
		}
	}
}

func (c *conn) publishCycle() error {
	vhost, _ := parseVhost(c.clientConnectInfo.tcUrl)
	// TODO: vhost定制配置
	streamKey := genStreamKey(vhost, c.clientConnectInfo.app, c.clientPublishOrPlayInfo.stream)
	sessionId := "E21A4829-6AAF-43FF-8405-3DB2B470FA13" //TODO: uuid or 调度赋值？

	sess, err := c.server.broker.createSession(c, vhost, streamKey, sessionId)
	if err != nil {
		return errors.Wrap(err, "create session in server's broker")
	} else {
		c.sess = sess
	}

	defer func() {
		if err := c.server.broker.softDelSession(streamKey); err != nil {
			c.server.logger.Error("soft delete session", zap.Error(err))
		}
	}()

	for {
		msg, err := c.Connection.RecvIntegralMessage() //TODO: 超时控制，配置net.Conn读超时?
		if err != nil {
			return errors.Wrap(err, "recv intergral message")
		}

		if err := c.onRecvIntegralMessage(msg); err != nil {
			return errors.Wrap(err, "on recv intergral message") //TODO: 超时控制，配置net.Conn写超时?
		}
	}
}

func (c *conn) playCycle() error {
	vhost, _ := parseVhost(c.clientConnectInfo.tcUrl)
	// TODO: vhost定制配置
	streamKey := genStreamKey(vhost, c.clientConnectInfo.app, c.clientPublishOrPlayInfo.stream)
	player, err := c.server.broker.addSessionPlayer(c, streamKey)
	if err != nil {
		return errors.Wrap(err, "add session player in server's broker")
	}

	return player.doPlaying()
}

func (c *conn) onRecvIntegralMessage(msg *chunk.Stream) error {
	defer msg.Reset()

	//response windows ack
	if err := c.Connection.ResponseAcknowledgementMessage(); err != nil {
		return errors.Wrap(err, "response ack")
	}

	messageTypeId := msg.GetChunkMessageTypeID()
	switch messageTypeId {
	case chunk.MsgSetChunkSize:
		c.Connection.HandleSetChunkSizeMessage(msg)
	case chunk.MsgAbortMessage:
		// TODO:
	case chunk.MsgAcknowledgement:
		c.Connection.HandleAcknowledgementMessage(msg)
	case chunk.MsgUserControlMessage:
		// TODO:
	case chunk.MsgWindowAcknowledgementSize:
		c.Connection.HandleWindowAcknowledgementSizeMessage(msg)
	case chunk.MsgSetPeerBandwidth:
		// TODO:
	case chunk.MsgAudioMessage, chunk.MsgVideoMessage:
		if c.sess != nil {
			if err := c.sess.onRecvAVMessage(msg, messageTypeId); err != nil {
				return errors.Wrap(err, "on Recv audio/video message")
			}
		} else if c.clientPublishOrPlayInfo.clientType != 1 {
			return errors.New("recv audio/video message but client isn't publisher")
		}
	case chunk.MsgAMF0CommandMessage, chunk.MsgAMF3CommandMessage:
		// decode command message
		if err := c.handleCommandMessage(msg, messageTypeId); err != nil {
			return errors.Wrap(err, "handle command message")
		}
	case chunk.MSGAMF0DataMessage, chunk.MsgAMF3DataMessage:
		if c.sess != nil {
			if err := c.sess.onRecvDataMessage(msg, messageTypeId); err != nil {
				return errors.Wrap(err, "on recv data message")
			}
		}
	}

	return nil
}

func (c *conn) handleCommandMessage(msg *chunk.Stream, typeId chunk.RtmpMessageTypeID) error {
	if typeId == chunk.MsgAMF3CommandMessage && len(msg.ChunkData) > 1 {
		// skip 1 byte to decode the amf3 command.
		msg.ChunkData = msg.ChunkData[1:]
	}

	r := bytes.NewReader(msg.ChunkData)
	vs, err := c.Connection.AmfDecoder.DecodeBatch(r, amf.Version(amf.AMF0))
	if err != nil && err != io.EOF {
		return errors.Wrap(err, "decode command message")
	}

	if cmd, ok := vs[0].(string); ok {
		c.server.logger.Debug("", zap.String("cmd", cmd))

		switch cmd {
		case "connect":
			if err := c.handleConnectCommandMessage(msg, vs[1:]); err != nil {
				return errors.Wrap(err, "handle connect command message")
			}
			c.server.logger.Debug("handle connect command message success")
		case "releaseStream":
			c.server.logger.Debug("do nothing whlie recv releaseStream command message")
		case "FCPublish":
			c.server.logger.Debug("do nothing whlie recv FCPublish command message")
		case "createStream":
			if err := c.handleCreateStreamCommandMessage(msg, vs[1:]); err != nil {
				return errors.Wrap(err, "handle createStream command message")
			}
			c.server.logger.Debug("handle createStream command message success.")
		case "publish":
			if err := c.handlePublishCommandMessage(msg, vs[1:]); err != nil {
				return errors.Wrap(err, "handle publish command message")
			}
			c.server.logger.Debug("handle publish command message success.")
		case "play":
			if err := c.handlePlayCommandMessage(msg, vs[1:]); err != nil {
				return errors.Wrap(err, "handle play command message")
			}
			c.server.logger.Debug("handle play command message success.")
		case "FCUnpublish":
			//TODO:
		case "deleteStream":
			//TODO:
		}
	}

	return nil
}

func (c *conn) handleConnectCommandMessage(msg *chunk.Stream, vs []interface{}) error {
	for _, v := range vs {
		switch v := v.(type) {
		case string:
		case float64:
			c.Connection.TransactionID = int(v)
			if c.TransactionID != 1 {
				return errors.Errorf("client connect message, Transaction ID is %d(not 1)", c.Connection.TransactionID)
			}
		case amf.Object:
			if app, ok := v["app"].(string); ok {
				c.clientConnectInfo.app = app
			}

			if flashVer, ok := v["flashVer"].(string); ok {
				c.clientConnectInfo.flashVer = flashVer
			}

			if swfUrl, ok := v["swfUrl"].(string); ok {
				c.clientConnectInfo.swfUrl = swfUrl
			}

			if tcUrl, ok := v["tcUrl"].(string); ok {
				c.clientConnectInfo.tcUrl = tcUrl
			}

			if fpad, ok := v["fpad"].(bool); ok {
				c.clientConnectInfo.fpad = fpad
			}

			if audioCodecs, ok := v["audioCodecs"].(int); ok {
				c.clientConnectInfo.audioCodecs = audioCodecs
			}

			if videoCodecs, ok := v["videoCodecs"].(int); ok {
				c.clientConnectInfo.videoCodecs = videoCodecs
			}

			if videoFunction, ok := v["videoFunction"].(int); ok {
				c.clientConnectInfo.videoFunction = videoFunction
			}

			if pageUrl, ok := v["pageUrl"].(string); ok {
				c.clientConnectInfo.pageUrl = pageUrl
			}

			if objectEncoding, ok := v["objectEncoding"].(float64); ok {
				c.clientConnectInfo.objectEncoding = int(objectEncoding)
			}
		}
	}

	// 检查解析到的connect命令消息结果
	if c.clientConnectInfo.app == "" || c.clientConnectInfo.tcUrl == "" { //TODO:如果服务app固定，此处即可拦截非法app
		return errors.Errorf("app and tcUrl params required while handle connect command message")
	}

	if c.clientConnectInfo.flashVer == "" || c.clientConnectInfo.swfUrl == "" {
		//TODO: warn
		_ = c.flashVer
	}

	/** 响应connect命令 **/
	if err := c.respConnectCommandMessage(msg); err != nil {
		return errors.Wrap(err, "response connect command message")
	}

	return nil
}

func (c *conn) respConnectCommandMessage(msg *chunk.Stream) error {
	totalBytes := 0

	respMsg, _ := chunk.NewProcotolControlMessage(
		chunk.MsgWindowAcknowledgementSize,
		4,
		c.Connection.OutAckSize.WindowSize,
	)
	if nw, err := c.Connection.SendIntegralMessage(respMsg); err != nil {
		return errors.Wrap(err, "send WindowAcknowledgementSize message")
	} else {
		totalBytes += nw
	}

	respMsg, _ = chunk.NewProcotolControlMessage(chunk.MsgSetPeerBandwidth, 5, 2500000)
	respMsg.ChunkData[4] = 2 // dynamic
	if nw, err := c.Connection.SendIntegralMessage(respMsg); err != nil {
		return errors.Wrap(err, "send SetPeerBandwidth message")
	} else {
		totalBytes += nw
	}

	respMsg, _ = chunk.NewProcotolControlMessage(chunk.MsgSetChunkSize, 4, c.LocalChunkSize)
	if nw, err := c.Connection.SendIntegralMessage(respMsg); err != nil {
		return errors.Wrap(err, "send SetPeerBandwidth message")
	} else {
		totalBytes += nw
	}

	resp := make(amf.Object)
	resp["fmsVer"] = "FMS/3,0,1,123"
	resp["capabilities"] = 31

	event := make(amf.Object)
	event["level"] = "status"
	event["code"] = "NetConnection.Connect.Success"
	event["description"] = "Connection succeeded."
	event["objectEncoding"] = c.objectEncoding
	cmdMsg, _ := chunk.NewCommandMessage(
		c.Connection.AmfEncoder,
		msg.GetChunkCsid(),
		msg.GetChunkMessageStreamID(),
		"_result", c.Connection.TransactionID, resp, event,
	)
	if nw, err := c.Connection.SendIntegralMessage(cmdMsg); err != nil {
		return errors.Wrap(err, "send NetConnection.Connect.Success message")
	} else {
		totalBytes += nw
	}

	if nf, err := c.Connection.Flush(); err != nil {
		return errors.Wrap(err, "flush response connect command message data")
	} else {
		if nf != int64(totalBytes) {
			return errors.Errorf("need write %d, actual: %d", totalBytes, nf)
		}
	}

	return nil
}

func (c *conn) handleCreateStreamCommandMessage(msg *chunk.Stream, vs []interface{}) error {
	for _, v := range vs {
		switch v := v.(type) {
		case float64:
			c.Connection.TransactionID = int(v)
		}
	}

	if err := c.respCreateStreamCommandMessage(msg); err != nil {
		return errors.Wrap(err, "response createStream command Message")
	}

	return nil
}

func (c *conn) respCreateStreamCommandMessage(msg *chunk.Stream) error {
	cmdMsg, _ := chunk.NewCommandMessage(
		c.Connection.AmfEncoder,
		msg.GetChunkCsid(),
		msg.GetChunkMessageStreamID(),
		"_result", c.TransactionID, nil, 1,
	)

	if nw, err := c.Connection.SendIntegralMessage(cmdMsg); err != nil {
		return errors.Wrap(err, "send createStream _result message")
	} else {
		nf, err := c.Connection.Flush()
		if err != nil {
			return errors.Wrap(err, "flush createStream _result message data")
		}

		if nf != int64(nw) {
			return errors.Errorf("need write %d, actual: %d", nw, nf)
		}
	}

	return nil
}

func (c *conn) handlePublishCommandMessage(msg *chunk.Stream, vs []interface{}) error {
	for k, v := range vs {
		switch v := v.(type) {
		case string:
			if k == 2 {
				c.clientPublishOrPlayInfo.stream = v
			} else if k == 3 {
				c.clientPublishOrPlayInfo.app = v
			}
		case float64:
			c.Connection.TransactionID = int(v)
		}
	}

	if c.clientPublishOrPlayInfo.stream == "" {
		return errors.New("stream empty after publish command message decoded")
	}
	c.clientPublishOrPlayInfo.clientType = 1

	if err := c.respPublishCommandMessage(msg); err != nil {
		return errors.Wrap(err, "response publish command message")
	}

	return nil
}

func (c *conn) respPublishCommandMessage(msg *chunk.Stream) error {
	event := make(amf.Object)
	event["level"] = "status"
	event["code"] = "NetStream.Publish.Start"
	event["description"] = "Start publising."

	cmdMsg, _ := chunk.NewCommandMessage(
		c.Connection.AmfEncoder,
		msg.GetChunkCsid(),
		msg.GetChunkMessageStreamID(),
		"onStatus", 0, nil, event,
	)

	if nw, err := c.Connection.SendIntegralMessage(cmdMsg); err != nil {
		return errors.Wrap(err, "send NetStream.Publish.Start command message")
	} else {
		nf, err := c.Connection.Flush()
		if err != nil {
			return errors.Wrap(err, "flush NetStream.Publish.Start command message data")
		}

		if nf != int64(nw) {
			return errors.Errorf("need write %d, actual: %d", nw, nf)
		}
	}

	return nil
}

func (c *conn) handlePlayCommandMessage(msg *chunk.Stream, vs []interface{}) error {
	for k, v := range vs {
		switch v := v.(type) {
		case string:
			if k == 2 {
				c.clientPublishOrPlayInfo.stream = v
			} else if k == 3 {
				c.clientPublishOrPlayInfo.app = v
			}
		case float64:
			c.Connection.TransactionID = int(v)
		}
	}

	if c.clientPublishOrPlayInfo.stream == "" {
		return errors.New("stream empty after play command message decoded")
	}
	c.clientPublishOrPlayInfo.clientType = 2

	if err := c.respPlayCommandMessage(msg); err != nil {
		return errors.Wrap(err, "response play command message")
	}

	return nil
}

func (c *conn) respPlayCommandMessage(msg *chunk.Stream) error {
	totalBytes := 0

	// set recorded
	ucMsg, _ := chunk.NewlUserControlMessage(4, 6) //streamIsRecorded = 4
	if nw, err := c.Connection.SendIntegralMessage(ucMsg); err != nil {
		return errors.Wrap(err, "send streamIsRecorded user control message")
	} else {
		totalBytes += nw
	}

	// set begin
	ucMsg, _ = chunk.NewlUserControlMessage(0, 6) //streamBegin = 0
	if nw, err := c.Connection.SendIntegralMessage(ucMsg); err != nil {
		return errors.Wrap(err, "send streamBegin user control message")
	} else {
		totalBytes += nw
	}

	// NetStream.Play.Resetstream
	event := make(amf.Object)
	event["level"] = "status"
	event["code"] = "NetStream.Play.Reset"
	event["description"] = "Playing and resetting stream."
	cmdMsg, _ := chunk.NewCommandMessage(
		c.Connection.AmfEncoder,
		msg.GetChunkCsid(),
		msg.GetChunkMessageStreamID(),
		"onStatus", 0, nil, event,
	)
	if nw, err := c.Connection.SendIntegralMessage(cmdMsg); err != nil {
		return errors.Wrap(err, "send NetStream.Play.Reset command message")
	} else {
		totalBytes += nw
	}

	// NetStream.Play.Start
	event["level"] = "status"
	event["code"] = "NetStream.Play.Start"
	event["description"] = "Started playing stream."
	cmdMsg, _ = chunk.NewCommandMessage(
		c.Connection.AmfEncoder,
		msg.GetChunkCsid(),
		msg.GetChunkMessageStreamID(),
		"onStatus", 0, nil, event,
	)
	if nw, err := c.Connection.SendIntegralMessage(cmdMsg); err != nil {
		return errors.Wrap(err, "send NetStream.Play.Start command message")
	} else {
		totalBytes += nw
	}

	// NetStream.Data.Start
	event["level"] = "status"
	event["code"] = "NetStream.Data.Start"
	event["description"] = "Started playing stream."
	cmdMsg, _ = chunk.NewCommandMessage(
		c.Connection.AmfEncoder,
		msg.GetChunkCsid(),
		msg.GetChunkMessageStreamID(),
		"onStatus", 0, nil, event,
	)
	if nw, err := c.Connection.SendIntegralMessage(cmdMsg); err != nil {
		return errors.Wrap(err, "send NetStream.Data.Start command message")
	} else {
		totalBytes += nw
	}

	// NetStream.Play.PublishNotify
	event["level"] = "status"
	event["code"] = "NetStream.Play.PublishNotify"
	event["description"] = "Started playing notify."
	cmdMsg, _ = chunk.NewCommandMessage(
		c.Connection.AmfEncoder,
		msg.GetChunkCsid(),
		msg.GetChunkMessageStreamID(),
		"onStatus", 0, nil, event,
	)
	if nw, err := c.Connection.SendIntegralMessage(cmdMsg); err != nil {
		return errors.Wrap(err, "send NetStream.Play.PublishNotify command message")
	} else {
		totalBytes += nw
	}

	if nf, err := c.Connection.Flush(); err != nil {
		return errors.Wrap(err, "flush response play command message data")
	} else {
		if nf != int64(totalBytes) {
			return errors.Errorf("need write %d, actual: %d", totalBytes, nf)
		}
	}

	return nil
}

func (c *conn) handleDataMessage(msg *chunk.Stream, typeId chunk.RtmpMessageTypeID) error {
	if typeId == chunk.MsgAMF3DataMessage && len(msg.ChunkData) > 1 {
		msg.ChunkData = msg.ChunkData[1:]
	}

	r := bytes.NewReader(msg.ChunkData)
	vs, err := c.Connection.AmfDecoder.DecodeBatch(r, amf.Version(amf.AMF0))
	if err != nil && err != io.EOF {
		return errors.Wrap(err, "decode data message")
	}

	if cmd, ok := vs[0].(string); ok {
		switch cmd {
		case "@setDataFrame":
			if err := c.handleSetDataFrameDataMessage(msg, vs[1:]); err != nil {
				return errors.Wrap(err, "handle @setDataFrame data message")
			}
			c.server.logger.Info("", zap.Any("onMetaData", c.onMetaData))
		}
	}

	return nil
}

func (c *conn) handleSetDataFrameDataMessage(msg *chunk.Stream, vs []interface{}) error {
	for _, v := range vs {
		switch v := v.(type) {
		case amf.Object:
			if duration, ok := v["duration"].(float64); ok {
				c.onMetaData.Duration = duration
			}

			if fileSize, ok := v["fileSize"].(float64); ok {
				c.onMetaData.Filesize = fileSize
			}

			if encoder, ok := v["encoder"].(string); ok {
				c.onMetaData.Encoder = encoder
			}

			if width, ok := v["width"].(float64); ok {
				c.onMetaData.Width = width
			}

			if height, ok := v["height"].(float64); ok {
				c.onMetaData.Height = height
			}

			if videocodecid, ok := v["videocodecid"].(float64); ok {
				c.onMetaData.VideoCodecID = int(videocodecid)
			}

			if framerate, ok := v["framerate"].(float64); ok {
				c.onMetaData.Framerate = framerate
			}

			if videodatarate, ok := v["videodatarate"].(float64); ok {
				c.onMetaData.VideodataRate = videodatarate
			}

			if audiocodecid, ok := v["audiocodecid"].(float64); ok {
				c.onMetaData.AudioCodecID = int(audiocodecid)
			}

			if audiochannels, ok := v["audiochannels"].(float64); ok {
				c.onMetaData.Audiochannels = int(audiochannels)
			}

			if stereo, ok := v["stereo"].(bool); ok {
				c.onMetaData.Stereo = stereo
			}

			if audiodatarate, ok := v["audiodatarate"].(float64); ok {
				c.onMetaData.Audiodatarate = audiodatarate
			}

			if audiosamplerate, ok := v["audiosamplerate"].(float64); ok {
				c.onMetaData.Audiosamplerate = audiosamplerate
			}

			if audiosamplesize, ok := v["audiosamplesize"].(float64); ok {
				c.onMetaData.Audiosamplesize = audiosamplesize
			}
		}
	}

	return nil
}

func (c *conn) handshake() error {
	handshakeComplete := func(handshakeStatus uint32) bool {
		return atomic.LoadUint32(&handshakeStatus) == 1
	}

	c.handshakeMutex.Lock()
	defer c.handshakeMutex.Unlock()

	if c.handshakeErr != nil {
		return c.handshakeErr
	}

	if handshakeComplete(c.handshakeStatus) {
		return nil
	}

	c.handshakeErr = handshake.WithClient(c.Connection.Rwc, c.server.config.HandshakeTimeout)
	if c.handshakeErr == nil {
		c.handshakeStatus++
	}

	if c.handshakeErr == nil && !handshakeComplete(c.handshakeStatus) {
		c.handshakeErr = errors.New("rtmp: internal error: handshake should be had a result")
	}

	return c.handshakeErr
}

func newServerConn(opts ...serverConnOption) (*conn, error) {
	c, err := (&conn{}).loadOptions(opts...)
	if err != nil {
		return nil, errors.Wrap(err, "load options")

	}

	if err := c.Connection.Init(); err != nil {
		return nil, errors.Wrap(err, "init connection")
	}

	return c, nil
}

var (
	errServer = errors.New("server conn belongs server required")
)

func (c *conn) loadOptions(opts ...serverConnOption) (*conn, error) {
	for _, opt := range opts {
		opt(c)
	}

	if c.server == nil {
		return nil, errServer
	}

	c.Connection.Logger = c.server.logger

	return c, nil
}

type serverConnOption func(*conn)

func WithServerConnServer(server *Server) serverConnOption {
	return func(c *conn) {
		c.server = server
	}
}

func WithServerConnRawConn(rwc net.Conn) serverConnOption {
	return func(c *conn) {
		c.Connection.Rwc = rwc // must
	}
}

func WithServerConnReadBufSize(size int) serverConnOption {
	return func(c *conn) {
		c.Connection.ReadBufSize = size //option
	}
}

func WithServerConnLocalChunkSize(size uint32) serverConnOption {
	return func(c *conn) {
		c.Connection.LocalChunkSize = size
	}
}

func WithServerConnReadHdrPoll(sp *sync.Pool) serverConnOption {
	return func(c *conn) {
		c.Connection.DecodeHdrPool = sp
	}
}

func WithServerConnChunkEncodePool(sp *sync.Pool) serverConnOption {
	return func(c *conn) {
		c.Connection.EncodeHdrPool = sp
	}
}

func WithServerConnNewChunkPool(sp *sync.Pool) serverConnOption {
	return func(c *conn) {
		c.NewChunkPool = sp
	}
}
