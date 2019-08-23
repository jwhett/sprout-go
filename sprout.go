package sprout

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"

	forest "git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
)

const (
	major = 0
	minor = 0
)

type MessageID int

type SproutConn struct {
	net.Conn
	Major, Minor  int
	nextMessageID MessageID
}

func New(transport net.Conn) (*SproutConn, error) {
	s := &SproutConn{
		Major:         major,
		Minor:         minor,
		nextMessageID: 0,
		Conn:          transport,
	}
	return s, nil
}

func (s *SproutConn) writeMessage(errorCtx, format string, fmtArgs ...interface{}) (messageID MessageID, err error) {
	messageID = s.nextMessageID
	s.nextMessageID++
	return s.writeMessageWithID(messageID, errorCtx, format, fmtArgs...)
}

func (s *SproutConn) writeMessageWithID(messageIDIn MessageID, errorCtx, format string, fmtArgs ...interface{}) (messageID MessageID, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf(errorCtx, err)
		}
	}()
	opts := make([]interface{}, 1, len(fmtArgs)+1)
	opts[0] = messageIDIn
	opts = append(opts, fmtArgs...)
	messageID = messageIDIn
	_, err = fmt.Fprintf(s, format, opts...)
	return messageID, err
}

func (s *SproutConn) SendVersion() (MessageID, error) {
	return s.writeMessage("failed to send version: %v", "version %d %d.%d\n", s.Major, s.Minor)
}

func (s *SproutConn) SendQueryAny(nodeType fields.NodeType, quantity int) (MessageID, error) {
	return s.writeMessage("failed to send query_any: %v", "query_any %d %d %d\n", nodeType, quantity)
}

func (s *SproutConn) SendQuery(nodeIds ...*fields.QualifiedHash) (MessageID, error) {
	builder := &strings.Builder{}
	for _, nodeId := range nodeIds {
		b, _ := nodeId.MarshalText()
		_, _ = builder.Write(b)
		builder.WriteString("\n")
	}
	return s.writeMessage("failed to send query: %v", "query %d %d\n%s", len(nodeIds), builder.String())
}

type AncestryRequest struct {
	*fields.QualifiedHash
	Levels int
}

func (r AncestryRequest) String() string {
	b, _ := r.QualifiedHash.MarshalText()
	return fmt.Sprintf("%d %s\n", r.Levels, string(b))
}

func (s *SproutConn) SendAncestry(reqs ...AncestryRequest) (MessageID, error) {
	builder := &strings.Builder{}
	for _, req := range reqs {
		builder.WriteString(req.String())
	}
	return s.writeMessage("failed to send ancestry: %v", "ancestry %d %d\n%s", len(reqs), builder.String())
}

func (s *SproutConn) SendLeavesOf(nodeId *fields.QualifiedHash, quantity int) (MessageID, error) {
	id, _ := nodeId.MarshalText()
	return s.writeMessage("failed to send leaves_of: %v", "leaves_of %d %s %d\n", string(id), quantity)
}

func NodeLine(n forest.Node) string {
	id, _ := n.ID().MarshalText()
	data, _ := n.MarshalBinary()
	return fmt.Sprintf("%s %s\n", string(id), base64.URLEncoding.EncodeToString(data))
}

func (s *SproutConn) SendResponse(msgID MessageID, index int, nodes []forest.Node) (MessageID, error) {
	builder := &strings.Builder{}
	for _, n := range nodes {
		builder.WriteString(NodeLine(n))
	}
	return s.writeMessageWithID(msgID, "failed to send response: %v", "response %d[%d] %d\n%s", index, len(nodes), builder.String())
}

func (s *SproutConn) subscribeOp(operation string, communities []*forest.Community) (MessageID, error) {
	builder := &strings.Builder{}
	for _, community := range communities {
		id, _ := community.ID().MarshalText()
		builder.WriteString(string(id))
		builder.WriteString("\n")
	}
	return s.writeMessage("failed to send "+operation+": %v", operation+" %d %d\n%s", len(communities), builder.String())
}

func (s *SproutConn) SendSubscribe(communities []*forest.Community) (MessageID, error) {
	return s.subscribeOp("subscribe", communities)
}

func (s *SproutConn) SendUnsubscribe(communities []*forest.Community) (MessageID, error) {
	return s.subscribeOp("unsubscribe", communities)
}

type ErrorCode int

const (
	ErrorMalformed ErrorCode = iota
)

func (s *SproutConn) SendError(targetMessageID MessageID, errorCode ErrorCode) (MessageID, error) {
	return s.writeMessageWithID(targetMessageID, "failed to send error: %v", "error %d %d", errorCode)
}

func (s *SproutConn) SendErrorPart(targetMessageID MessageID, index int, errorCode ErrorCode) (MessageID, error) {
	return s.writeMessageWithID(targetMessageID, "failed to send error: %v", "error_part %d[%d] %d", index, errorCode)
}

func (s *SproutConn) SendOkPart(targetMessageID MessageID, index int) (MessageID, error) {
	return s.writeMessageWithID(targetMessageID, "failed to send ok: %v", "ok_part %d[%d] %d", index)
}

func (s *SproutConn) SendAnnounce(nodes []forest.Node) (messageID MessageID, err error) {
	builder := &strings.Builder{}
	for _, node := range nodes {
		id := node.ID()
		b, _ := id.MarshalText()
		n, _ := node.MarshalBinary()
		enc := base64.URLEncoding.EncodeToString(n)
		_, _ = builder.Write(b)
		_, _ = builder.WriteString(enc)
		builder.WriteString("\n")
	}

	return s.writeMessage("failed to make announcement: %v", "announce %d %d\n%s", len(nodes), builder.String())
}