package keeper

import (
	collcodec "cosmossdk.io/collections/codec"

	"github.com/cosmos/cosmos-sdk/codec"
	gogoproto "github.com/cosmos/gogoproto/proto"
)

// collPtrValue returns a collections ValueCodec that stores a gogoproto message
// by pointer (*T). The cosmos-sdk codec.CollValue helper returns a value codec
// (ValueCodec[T]); the credits state schema and keeper were written against
// pointer values (*types.Lock, *types.SettlementRecord, ...), so this thin
// adapter preserves those pointer semantics after the protobuf-go→gogoproto
// migration without rewriting every keeper read/write. It mirrors the SDK's own
// gogo collValue, differing only in that it stores/returns PT instead of T.
func collPtrValue[T any, PT interface {
	*T
	gogoproto.Message
}](cdc codec.BinaryCodec) collcodec.ValueCodec[PT] {
	return collPtrValueCodec[T, PT]{
		cdc:         cdc.(codec.Codec),
		messageName: gogoproto.MessageName(PT(new(T))),
	}
}

type collPtrValueCodec[T any, PT interface {
	*T
	gogoproto.Message
}] struct {
	cdc         codec.Codec
	messageName string
}

func (c collPtrValueCodec[T, PT]) Encode(value PT) ([]byte, error) {
	return c.cdc.Marshal(value)
}

func (c collPtrValueCodec[T, PT]) Decode(b []byte) (PT, error) {
	value := PT(new(T))
	if err := c.cdc.Unmarshal(b, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (c collPtrValueCodec[T, PT]) EncodeJSON(value PT) ([]byte, error) {
	return c.cdc.MarshalJSON(value)
}

func (c collPtrValueCodec[T, PT]) DecodeJSON(b []byte) (PT, error) {
	value := PT(new(T))
	if err := c.cdc.UnmarshalJSON(b, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (c collPtrValueCodec[T, PT]) Stringify(value PT) string {
	return value.String()
}

func (c collPtrValueCodec[T, PT]) ValueType() string {
	return "github.com/cosmos/gogoproto/" + c.messageName
}
