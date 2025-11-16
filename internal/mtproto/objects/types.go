// Copyright (c) 2024 RoseLoverX

package objects

// Some types are decoded in a very specific way, so their decoding logic is stored here and only here.
import (
	"bytes"
	"compress/gzip"
	"fmt"

	"github.com/amarnathcjd/gogram/internal/encoding/tl"
	"github.com/amarnathcjd/gogram/internal/mtproto/messages"
)

// TYPES

// Null is an empty object used for transmission in TL channels. It signifies that a response is not expected.
type Null struct{}

func (*Null) CRC() uint32 {
	panic("makes no sense")
}

type ResPQ struct {
	Nonce        *tl.Int128
	ServerNonce  *tl.Int128
	Pq           []byte
	Fingerprints []int64
}

func (*ResPQ) CRC() uint32 {
	return 0x05162463
}

type PQInnerData struct {
	Pq          []byte
	P           []byte
	Q           []byte
	Nonce       *tl.Int128
	ServerNonce *tl.Int128
	NewNonce    *tl.Int256
}

func (*PQInnerData) CRC() uint32 {
	return 0x83c95aec
}

// PQInnerDataTempDc represents p_q_inner_data_temp_dc used for generating
// temporary authorization keys (PFS) as described in MTProto auth_key docs.
//
// p_q_inner_data_temp_dc#56fddf88 pq:string p:string q:string nonce:int128
//   server_nonce:int128 new_nonce:int256 dc:int expires_in:int = P_Q_inner_data;
func (*PQInnerDataTempDc) CRC() uint32 {
	return 0x56fddf88
}

type PQInnerDataTempDc struct {
	Pq          []byte
	P           []byte
	Q           []byte
	Nonce       *tl.Int128
	ServerNonce *tl.Int128
	NewNonce    *tl.Int256
	Dc          int32
	ExpiresIn   int32
}

// BindAuthKeyInner represents the bind_auth_key_inner message used to bind
// a temporary auth key to a permanent one in auth.bindTempAuthKey.
//
// bind_auth_key_inner#75a3f765 nonce:long temp_auth_key_id:long
//   perm_auth_key_id:long temp_session_id:long expires_at:int = BindAuthKeyInner;
type BindAuthKeyInner struct {
	Nonce         int64
	TempAuthKeyID int64
	PermAuthKeyID int64
	TempSessionID int64
	ExpiresAt     int32
}

func (*BindAuthKeyInner) CRC() uint32 {
	return 0x75a3f765
}

type ServerDHParams interface {
	tl.Object
	ImplementsServerDHParams()
}

type ServerDHParamsFail struct {
	Nonce        *tl.Int128
	ServerNonce  *tl.Int128
	NewNonceHash *tl.Int128
}

func (*ServerDHParamsFail) ImplementsServerDHParams() {}

func (*ServerDHParamsFail) CRC() uint32 {
	return 0x79cb045d
}

type ServerDHParamsOk struct {
	Nonce           *tl.Int128
	ServerNonce     *tl.Int128
	EncryptedAnswer []byte
}

func (*ServerDHParamsOk) ImplementsServerDHParams() {}

func (*ServerDHParamsOk) CRC() uint32 {
	return 0xd0e8075c
}

type ServerDHInnerData struct {
	Nonce       *tl.Int128
	ServerNonce *tl.Int128
	G           int32
	DhPrime     []byte
	GA          []byte
	ServerTime  int32
}

func (*ServerDHInnerData) CRC() uint32 {
	return 0xb5890dba
}

type ClientDHInnerData struct {
	Nonce       *tl.Int128
	ServerNonce *tl.Int128
	Retry       int64
	GB          []byte
}

func (*ClientDHInnerData) CRC() uint32 {
	return 0x6643b654
}

type DHGenOk struct {
	Nonce         *tl.Int128
	ServerNonce   *tl.Int128
	NewNonceHash1 *tl.Int128
}

func (t *DHGenOk) ImplementsSetClientDHParamsAnswer() {}

func (*DHGenOk) CRC() uint32 {
	return 0x3bcbf734
}

type SetClientDHParamsAnswer interface {
	tl.Object
	ImplementsSetClientDHParamsAnswer()
}

type DHGenRetry struct {
	Nonce         *tl.Int128
	ServerNonce   *tl.Int128
	NewNonceHash2 *tl.Int128
}

func (*DHGenRetry) ImplementsSetClientDHParamsAnswer() {}

func (*DHGenRetry) CRC() uint32 {
	return 0x46dc1fb9
}

type DHGenFail struct {
	Nonce         *tl.Int128
	ServerNonce   *tl.Int128
	NewNonceHash3 *tl.Int128
}

func (*DHGenFail) ImplementsSetClientDHParamsAnswer() {}

func (*DHGenFail) CRC() uint32 {
	return 0xa69dae02
}

type RpcResult struct {
	ReqMsgID int64
	Obj      tl.Object
}

func (*RpcResult) CRC() uint32 {
	return CrcRpcResult
}

// func (t *RpcResult) DecodeFromButItsVector(d *Decoder, as reflect.Type) {
// 	t.ReqMsgID = d.PopLong()
// 	crc := binary.LittleEndian.Uint32(d.GetRestOfMessage()[:WordLen])
//
// 	if crc == CrcGzipPacked {
// 		_ = d.PopCRC()
// 		gz := &GzipPacked{}
// 		gz.DecodeFromButItsVector(d, as)
// 		t.Obj = gz.Obj.(*InnerVectorObject)
// 	} else {
// 		vector := d.PopVector(as)
// 		t.Obj = &InnerVectorObject{I: vector}
// 	}
// }

type RpcError struct {
	ErrorCode    int32
	ErrorMessage string
}

func (*RpcError) CRC() uint32 {
	return 0x2144ca19
}

type RpcDropAnswer interface {
	tl.Object
	ImplementsRpcDropAnswer()
}

type RpcAnswerUnknown struct{}

func (*RpcAnswerUnknown) ImplementsRpcDropAnswer() {}

func (*RpcAnswerUnknown) CRC() uint32 {
	return 0x5e2ad36e
}

type RpcAnswerDroppedRunning struct{}

func (*RpcAnswerDroppedRunning) ImplementsRpcDropAnswer() {}

func (*RpcAnswerDroppedRunning) CRC() uint32 {
	return 0xcd78e586
}

type RpcAnswerDropped struct {
	MsgID int64
	SewNo int32
	Bytes int32
}

func (*RpcAnswerDropped) ImplementsRpcDropAnswer() {}

func (*RpcAnswerDropped) CRC() uint32 {
	return 0xa43ad8b7
}

type FutureSalt struct {
	ValidSince int32
	ValidUntil int32
	Salt       int64
}

func (*FutureSalt) CRC() uint32 {
	return 0x0949d9dc
}

type FutureSalts struct {
	ReqMsgID int64
	Now      int32
	Salts    []*FutureSalt
}

func (*FutureSalts) CRC() uint32 {
	return 0xae500895
}

type Pong struct {
	MsgID  int64
	PingID int64
}

func (*Pong) CRC() uint32 {
	return 0x347773c5
}

// destroy_session_ok#e22045fc session_id:long = DestroySessionRes;
// destroy_session_none#62d350c9 session_id:long = DestroySessionRes;

type NewSessionCreated struct {
	FirstMsgID int64
	UniqueID   int64
	ServerSalt int64
}

func (*NewSessionCreated) CRC() uint32 {
	return 0x9ec20908
}

// This is an exception to the usual vector handling rules.
//
// The data is encoded as `msg_container#73f1f8dc messages:vector<%Message> = MessageContainer;`.
// It appears that `<%Type>` indicates a possible implicit vector, but this behavior is not fully understood.
//
// It's possible that the developers were not thinking clearly at this point, but it's difficult to say for sure.
type MessageContainer []*messages.Encrypted

func (*MessageContainer) CRC() uint32 {
	return 0x73f1f8dc
}

func (t *MessageContainer) MarshalTL(e *tl.Encoder) error {
	e.PutUint(t.CRC())
	e.PutInt(int32(len(*t)))
	if err := e.CheckErr(); err != nil {
		return err
	}

	for _, msg := range *t {
		e.PutLong(msg.MsgID)
		e.PutInt(msg.SeqNo)
		//       msgID        seqNo        len                object
		e.PutInt(tl.LongLen + tl.WordLen + tl.WordLen + int32(len(msg.Msg)))
		e.PutRawBytes(msg.Msg)
	}
	return e.CheckErr()
}

func (t *MessageContainer) UnmarshalTL(d *tl.Decoder) error {
	count := int(d.PopInt())
	arr := make([]*messages.Encrypted, count)
	for i := 0; i < count; i++ {
		msg := new(messages.Encrypted)
		msg.MsgID = d.PopLong()
		msg.SeqNo = d.PopInt()
		size := d.PopInt()
		msg.Msg = d.PopRawBytes(int(size))
		arr[i] = msg
	}
	*t = arr

	return nil
}

type Message struct {
	MsgID int64
	SeqNo int32
	Bytes int32
	Body  tl.Object
}

type MsgCopy struct {
	OrigMessage *Message
}

func (*MsgCopy) CRC() uint32 {
	return 0xe06046b2
}

type GzipPacked struct {
	Obj tl.Object
}

func (*GzipPacked) CRC() uint32 {
	return CrcGzipPacked
}

func (*GzipPacked) MarshalTL(_ *tl.Encoder) error {
	panic("not implemented")
}

func (t *GzipPacked) UnmarshalTL(d *tl.Decoder) error {
	obj, err := t.popMessageAsBytes(d)
	if err != nil {
		return err
	}

	t.Obj, err = tl.DecodeUnknownObject(obj)
	if err != nil {
		return fmt.Errorf("parsing gzipped object: %w", err)
	}

	return nil
}

func (*GzipPacked) popMessageAsBytes(d *tl.Decoder) ([]byte, error) {
	// TODO: The standard gzip package throws an error "gzip: invalid header".
	// After investigating, it appears that the gzip function is receiving a segment of data that is located
	// billions of bits away from the actual message. For example, the message starts with 0x1f 0x8b 0x08 0x00 ...,
	// but the gzip function is receiving a segment that is 500+ bytes ahead of the message start.
	//
	// This current implementation appears to work. Therefore, it is best not to modify it to avoid potentially
	// breaking functionality.

	decompressed := make([]byte, 0, 4096)

	var buf bytes.Buffer
	_, _ = buf.Write(d.PopMessage())
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		return nil, fmt.Errorf("creating gzip reader: %w", err)
	}

	b := make([]byte, 4096)
	for {
		n, _ := gz.Read(b)

		decompressed = append(decompressed, b[0:n]...)
		if n <= 0 {
			break
		}
	}

	return decompressed, nil
}

type MsgsAck struct {
	MsgIDs []int64
}

func (*MsgsAck) CRC() uint32 {
	return 0x62d6b459
}

type BadMsgNotification struct {
	BadMsgID    int64
	BadMsgSeqNo int32
	Code        int32
}

func (*BadMsgNotification) ImplementsBadMsgNotification() {}

func (*BadMsgNotification) CRC() uint32 {
	return 0xa7eff811
}

type BadServerSalt struct {
	BadMsgID    int64
	BadMsgSeqNo int32
	ErrorCode   int32
	NewSalt     int64
}

func (*BadServerSalt) ImplementsBadMsgNotification() {}

func (*BadServerSalt) CRC() uint32 {
	return 0xedab447b
}

// msg_new_detailed_info#809db6df answer_msg_id:long bytes:int status:int = MsgDetailedInfo;

type MsgResendReq struct {
	MsgIDs []int64
}

func (*MsgResendReq) CRC() uint32 {
	return 0x7d861a08
}

type MsgsStateReq struct {
	MsgIDs []int64
}

func (*MsgsStateReq) CRC() uint32 {
	return 0xda69fb52
}

type MsgsStateInfo struct {
	ReqMsgID int64
	Info     []byte
}

func (*MsgsStateInfo) CRC() uint32 {
	return 0x04deb57d
}

type MsgsAllInfo struct {
	MsgIDs []int64
	Info   []byte
}

func (*MsgsAllInfo) CRC() uint32 {
	return 0x8cc0d131
}

type MsgsDetailedInfo struct {
	MsgID       int64
	AnswerMsgID int64
	Bytes       int32
	Status      int32
}

func (*MsgsDetailedInfo) CRC() uint32 {
	return 0x276d3ec6
}

type MsgsNewDetailedInfo struct {
	AnswerMsgID int64
	Bytes       int32
	Status      int32
}

func (*MsgsNewDetailedInfo) CRC() uint32 {
	return 0x809db6df
}
