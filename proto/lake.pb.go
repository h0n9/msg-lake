// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.30.0
// 	protoc        v3.21.12
// source: proto/lake.proto

package proto

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type SubscribeResType int32

const (
	SubscribeResType_SUBSCRIBE_RES_TYPE_UNSPECIFIED SubscribeResType = 0
	SubscribeResType_SUBSCRIBE_RES_TYPE_ACK         SubscribeResType = 1
	SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY       SubscribeResType = 2
)

// Enum value maps for SubscribeResType.
var (
	SubscribeResType_name = map[int32]string{
		0: "SUBSCRIBE_RES_TYPE_UNSPECIFIED",
		1: "SUBSCRIBE_RES_TYPE_ACK",
		2: "SUBSCRIBE_RES_TYPE_RELAY",
	}
	SubscribeResType_value = map[string]int32{
		"SUBSCRIBE_RES_TYPE_UNSPECIFIED": 0,
		"SUBSCRIBE_RES_TYPE_ACK":         1,
		"SUBSCRIBE_RES_TYPE_RELAY":       2,
	}
)

func (x SubscribeResType) Enum() *SubscribeResType {
	p := new(SubscribeResType)
	*p = x
	return p
}

func (x SubscribeResType) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (SubscribeResType) Descriptor() protoreflect.EnumDescriptor {
	return file_proto_lake_proto_enumTypes[0].Descriptor()
}

func (SubscribeResType) Type() protoreflect.EnumType {
	return &file_proto_lake_proto_enumTypes[0]
}

func (x SubscribeResType) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use SubscribeResType.Descriptor instead.
func (SubscribeResType) EnumDescriptor() ([]byte, []int) {
	return file_proto_lake_proto_rawDescGZIP(), []int{0}
}

type PublishReq struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	TopicId    string      `protobuf:"bytes,1,opt,name=topic_id,json=topicId,proto3" json:"topic_id,omitempty"`
	MsgCapsule *MsgCapsule `protobuf:"bytes,2,opt,name=msg_capsule,json=msgCapsule,proto3" json:"msg_capsule,omitempty"`
}

func (x *PublishReq) Reset() {
	*x = PublishReq{}
	if protoimpl.UnsafeEnabled {
		mi := &file_proto_lake_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *PublishReq) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PublishReq) ProtoMessage() {}

func (x *PublishReq) ProtoReflect() protoreflect.Message {
	mi := &file_proto_lake_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PublishReq.ProtoReflect.Descriptor instead.
func (*PublishReq) Descriptor() ([]byte, []int) {
	return file_proto_lake_proto_rawDescGZIP(), []int{0}
}

func (x *PublishReq) GetTopicId() string {
	if x != nil {
		return x.TopicId
	}
	return ""
}

func (x *PublishReq) GetMsgCapsule() *MsgCapsule {
	if x != nil {
		return x.MsgCapsule
	}
	return nil
}

type PublishRes struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	TopicId string `protobuf:"bytes,1,opt,name=topic_id,json=topicId,proto3" json:"topic_id,omitempty"`
	Ok      bool   `protobuf:"varint,2,opt,name=ok,proto3" json:"ok,omitempty"`
}

func (x *PublishRes) Reset() {
	*x = PublishRes{}
	if protoimpl.UnsafeEnabled {
		mi := &file_proto_lake_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *PublishRes) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*PublishRes) ProtoMessage() {}

func (x *PublishRes) ProtoReflect() protoreflect.Message {
	mi := &file_proto_lake_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use PublishRes.ProtoReflect.Descriptor instead.
func (*PublishRes) Descriptor() ([]byte, []int) {
	return file_proto_lake_proto_rawDescGZIP(), []int{1}
}

func (x *PublishRes) GetTopicId() string {
	if x != nil {
		return x.TopicId
	}
	return ""
}

func (x *PublishRes) GetOk() bool {
	if x != nil {
		return x.Ok
	}
	return false
}

type SubscribeReq struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	TopicId    string      `protobuf:"bytes,1,opt,name=topic_id,json=topicId,proto3" json:"topic_id,omitempty"`
	MsgCapsule *MsgCapsule `protobuf:"bytes,2,opt,name=msg_capsule,json=msgCapsule,proto3" json:"msg_capsule,omitempty"`
}

func (x *SubscribeReq) Reset() {
	*x = SubscribeReq{}
	if protoimpl.UnsafeEnabled {
		mi := &file_proto_lake_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SubscribeReq) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SubscribeReq) ProtoMessage() {}

func (x *SubscribeReq) ProtoReflect() protoreflect.Message {
	mi := &file_proto_lake_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SubscribeReq.ProtoReflect.Descriptor instead.
func (*SubscribeReq) Descriptor() ([]byte, []int) {
	return file_proto_lake_proto_rawDescGZIP(), []int{2}
}

func (x *SubscribeReq) GetTopicId() string {
	if x != nil {
		return x.TopicId
	}
	return ""
}

func (x *SubscribeReq) GetMsgCapsule() *MsgCapsule {
	if x != nil {
		return x.MsgCapsule
	}
	return nil
}

type SubscribeRes struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Type         SubscribeResType `protobuf:"varint,1,opt,name=type,proto3,enum=SubscribeResType" json:"type,omitempty"`
	TopicId      string           `protobuf:"bytes,2,opt,name=topic_id,json=topicId,proto3" json:"topic_id,omitempty"`
	SubscriberId string           `protobuf:"bytes,3,opt,name=subscriber_id,json=subscriberId,proto3" json:"subscriber_id,omitempty"`
	// Types that are assignable to Res:
	//
	//	*SubscribeRes_Ok
	//	*SubscribeRes_MsgCapsule
	Res isSubscribeRes_Res `protobuf_oneof:"res"`
}

func (x *SubscribeRes) Reset() {
	*x = SubscribeRes{}
	if protoimpl.UnsafeEnabled {
		mi := &file_proto_lake_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SubscribeRes) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SubscribeRes) ProtoMessage() {}

func (x *SubscribeRes) ProtoReflect() protoreflect.Message {
	mi := &file_proto_lake_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SubscribeRes.ProtoReflect.Descriptor instead.
func (*SubscribeRes) Descriptor() ([]byte, []int) {
	return file_proto_lake_proto_rawDescGZIP(), []int{3}
}

func (x *SubscribeRes) GetType() SubscribeResType {
	if x != nil {
		return x.Type
	}
	return SubscribeResType_SUBSCRIBE_RES_TYPE_UNSPECIFIED
}

func (x *SubscribeRes) GetTopicId() string {
	if x != nil {
		return x.TopicId
	}
	return ""
}

func (x *SubscribeRes) GetSubscriberId() string {
	if x != nil {
		return x.SubscriberId
	}
	return ""
}

func (m *SubscribeRes) GetRes() isSubscribeRes_Res {
	if m != nil {
		return m.Res
	}
	return nil
}

func (x *SubscribeRes) GetOk() bool {
	if x, ok := x.GetRes().(*SubscribeRes_Ok); ok {
		return x.Ok
	}
	return false
}

func (x *SubscribeRes) GetMsgCapsule() *MsgCapsule {
	if x, ok := x.GetRes().(*SubscribeRes_MsgCapsule); ok {
		return x.MsgCapsule
	}
	return nil
}

type isSubscribeRes_Res interface {
	isSubscribeRes_Res()
}

type SubscribeRes_Ok struct {
	Ok bool `protobuf:"varint,4,opt,name=ok,proto3,oneof"`
}

type SubscribeRes_MsgCapsule struct {
	MsgCapsule *MsgCapsule `protobuf:"bytes,5,opt,name=msg_capsule,json=msgCapsule,proto3,oneof"`
}

func (*SubscribeRes_Ok) isSubscribeRes_Res() {}

func (*SubscribeRes_MsgCapsule) isSubscribeRes_Res() {}

type MsgCapsule struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Data      []byte     `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
	Signature *Signature `protobuf:"bytes,2,opt,name=signature,proto3" json:"signature,omitempty"`
}

func (x *MsgCapsule) Reset() {
	*x = MsgCapsule{}
	if protoimpl.UnsafeEnabled {
		mi := &file_proto_lake_proto_msgTypes[4]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MsgCapsule) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MsgCapsule) ProtoMessage() {}

func (x *MsgCapsule) ProtoReflect() protoreflect.Message {
	mi := &file_proto_lake_proto_msgTypes[4]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MsgCapsule.ProtoReflect.Descriptor instead.
func (*MsgCapsule) Descriptor() ([]byte, []int) {
	return file_proto_lake_proto_rawDescGZIP(), []int{4}
}

func (x *MsgCapsule) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

func (x *MsgCapsule) GetSignature() *Signature {
	if x != nil {
		return x.Signature
	}
	return nil
}

type Signature struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	PubKey []byte `protobuf:"bytes,1,opt,name=pub_key,json=pubKey,proto3" json:"pub_key,omitempty"`
	Data   []byte `protobuf:"bytes,2,opt,name=data,proto3" json:"data,omitempty"`
}

func (x *Signature) Reset() {
	*x = Signature{}
	if protoimpl.UnsafeEnabled {
		mi := &file_proto_lake_proto_msgTypes[5]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Signature) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Signature) ProtoMessage() {}

func (x *Signature) ProtoReflect() protoreflect.Message {
	mi := &file_proto_lake_proto_msgTypes[5]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Signature.ProtoReflect.Descriptor instead.
func (*Signature) Descriptor() ([]byte, []int) {
	return file_proto_lake_proto_rawDescGZIP(), []int{5}
}

func (x *Signature) GetPubKey() []byte {
	if x != nil {
		return x.PubKey
	}
	return nil
}

func (x *Signature) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

var File_proto_lake_proto protoreflect.FileDescriptor

var file_proto_lake_proto_rawDesc = []byte{
	0x0a, 0x10, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x6c, 0x61, 0x6b, 0x65, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x22, 0x55, 0x0a, 0x0a, 0x50, 0x75, 0x62, 0x6c, 0x69, 0x73, 0x68, 0x52, 0x65, 0x71,
	0x12, 0x19, 0x0a, 0x08, 0x74, 0x6f, 0x70, 0x69, 0x63, 0x5f, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x07, 0x74, 0x6f, 0x70, 0x69, 0x63, 0x49, 0x64, 0x12, 0x2c, 0x0a, 0x0b, 0x6d,
	0x73, 0x67, 0x5f, 0x63, 0x61, 0x70, 0x73, 0x75, 0x6c, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b,
	0x32, 0x0b, 0x2e, 0x4d, 0x73, 0x67, 0x43, 0x61, 0x70, 0x73, 0x75, 0x6c, 0x65, 0x52, 0x0a, 0x6d,
	0x73, 0x67, 0x43, 0x61, 0x70, 0x73, 0x75, 0x6c, 0x65, 0x22, 0x37, 0x0a, 0x0a, 0x50, 0x75, 0x62,
	0x6c, 0x69, 0x73, 0x68, 0x52, 0x65, 0x73, 0x12, 0x19, 0x0a, 0x08, 0x74, 0x6f, 0x70, 0x69, 0x63,
	0x5f, 0x69, 0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x74, 0x6f, 0x70, 0x69, 0x63,
	0x49, 0x64, 0x12, 0x0e, 0x0a, 0x02, 0x6f, 0x6b, 0x18, 0x02, 0x20, 0x01, 0x28, 0x08, 0x52, 0x02,
	0x6f, 0x6b, 0x22, 0x57, 0x0a, 0x0c, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x52,
	0x65, 0x71, 0x12, 0x19, 0x0a, 0x08, 0x74, 0x6f, 0x70, 0x69, 0x63, 0x5f, 0x69, 0x64, 0x18, 0x01,
	0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x74, 0x6f, 0x70, 0x69, 0x63, 0x49, 0x64, 0x12, 0x2c, 0x0a,
	0x0b, 0x6d, 0x73, 0x67, 0x5f, 0x63, 0x61, 0x70, 0x73, 0x75, 0x6c, 0x65, 0x18, 0x02, 0x20, 0x01,
	0x28, 0x0b, 0x32, 0x0b, 0x2e, 0x4d, 0x73, 0x67, 0x43, 0x61, 0x70, 0x73, 0x75, 0x6c, 0x65, 0x52,
	0x0a, 0x6d, 0x73, 0x67, 0x43, 0x61, 0x70, 0x73, 0x75, 0x6c, 0x65, 0x22, 0xbe, 0x01, 0x0a, 0x0c,
	0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x52, 0x65, 0x73, 0x12, 0x25, 0x0a, 0x04,
	0x74, 0x79, 0x70, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x11, 0x2e, 0x53, 0x75, 0x62,
	0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x52, 0x65, 0x73, 0x54, 0x79, 0x70, 0x65, 0x52, 0x04, 0x74,
	0x79, 0x70, 0x65, 0x12, 0x19, 0x0a, 0x08, 0x74, 0x6f, 0x70, 0x69, 0x63, 0x5f, 0x69, 0x64, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x74, 0x6f, 0x70, 0x69, 0x63, 0x49, 0x64, 0x12, 0x23,
	0x0a, 0x0d, 0x73, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x72, 0x5f, 0x69, 0x64, 0x18,
	0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0c, 0x73, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65,
	0x72, 0x49, 0x64, 0x12, 0x10, 0x0a, 0x02, 0x6f, 0x6b, 0x18, 0x04, 0x20, 0x01, 0x28, 0x08, 0x48,
	0x00, 0x52, 0x02, 0x6f, 0x6b, 0x12, 0x2e, 0x0a, 0x0b, 0x6d, 0x73, 0x67, 0x5f, 0x63, 0x61, 0x70,
	0x73, 0x75, 0x6c, 0x65, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x0b, 0x2e, 0x4d, 0x73, 0x67,
	0x43, 0x61, 0x70, 0x73, 0x75, 0x6c, 0x65, 0x48, 0x00, 0x52, 0x0a, 0x6d, 0x73, 0x67, 0x43, 0x61,
	0x70, 0x73, 0x75, 0x6c, 0x65, 0x42, 0x05, 0x0a, 0x03, 0x72, 0x65, 0x73, 0x22, 0x4a, 0x0a, 0x0a,
	0x4d, 0x73, 0x67, 0x43, 0x61, 0x70, 0x73, 0x75, 0x6c, 0x65, 0x12, 0x12, 0x0a, 0x04, 0x64, 0x61,
	0x74, 0x61, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x04, 0x64, 0x61, 0x74, 0x61, 0x12, 0x28,
	0x0a, 0x09, 0x73, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x0b, 0x32, 0x0a, 0x2e, 0x53, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x52, 0x09, 0x73,
	0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x22, 0x38, 0x0a, 0x09, 0x53, 0x69, 0x67, 0x6e,
	0x61, 0x74, 0x75, 0x72, 0x65, 0x12, 0x17, 0x0a, 0x07, 0x70, 0x75, 0x62, 0x5f, 0x6b, 0x65, 0x79,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x06, 0x70, 0x75, 0x62, 0x4b, 0x65, 0x79, 0x12, 0x12,
	0x0a, 0x04, 0x64, 0x61, 0x74, 0x61, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x04, 0x64, 0x61,
	0x74, 0x61, 0x2a, 0x70, 0x0a, 0x10, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x52,
	0x65, 0x73, 0x54, 0x79, 0x70, 0x65, 0x12, 0x22, 0x0a, 0x1e, 0x53, 0x55, 0x42, 0x53, 0x43, 0x52,
	0x49, 0x42, 0x45, 0x5f, 0x52, 0x45, 0x53, 0x5f, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x55, 0x4e, 0x53,
	0x50, 0x45, 0x43, 0x49, 0x46, 0x49, 0x45, 0x44, 0x10, 0x00, 0x12, 0x1a, 0x0a, 0x16, 0x53, 0x55,
	0x42, 0x53, 0x43, 0x52, 0x49, 0x42, 0x45, 0x5f, 0x52, 0x45, 0x53, 0x5f, 0x54, 0x59, 0x50, 0x45,
	0x5f, 0x41, 0x43, 0x4b, 0x10, 0x01, 0x12, 0x1c, 0x0a, 0x18, 0x53, 0x55, 0x42, 0x53, 0x43, 0x52,
	0x49, 0x42, 0x45, 0x5f, 0x52, 0x45, 0x53, 0x5f, 0x54, 0x59, 0x50, 0x45, 0x5f, 0x52, 0x45, 0x4c,
	0x41, 0x59, 0x10, 0x02, 0x32, 0x58, 0x0a, 0x04, 0x4c, 0x61, 0x6b, 0x65, 0x12, 0x23, 0x0a, 0x07,
	0x50, 0x75, 0x62, 0x6c, 0x69, 0x73, 0x68, 0x12, 0x0b, 0x2e, 0x50, 0x75, 0x62, 0x6c, 0x69, 0x73,
	0x68, 0x52, 0x65, 0x71, 0x1a, 0x0b, 0x2e, 0x50, 0x75, 0x62, 0x6c, 0x69, 0x73, 0x68, 0x52, 0x65,
	0x73, 0x12, 0x2b, 0x0a, 0x09, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x12, 0x0d,
	0x2e, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x52, 0x65, 0x71, 0x1a, 0x0d, 0x2e,
	0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x52, 0x65, 0x73, 0x30, 0x01, 0x42, 0x20,
	0x5a, 0x1e, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x68, 0x30, 0x6e,
	0x39, 0x2f, 0x6d, 0x73, 0x67, 0x2d, 0x6c, 0x61, 0x6b, 0x65, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_proto_lake_proto_rawDescOnce sync.Once
	file_proto_lake_proto_rawDescData = file_proto_lake_proto_rawDesc
)

func file_proto_lake_proto_rawDescGZIP() []byte {
	file_proto_lake_proto_rawDescOnce.Do(func() {
		file_proto_lake_proto_rawDescData = protoimpl.X.CompressGZIP(file_proto_lake_proto_rawDescData)
	})
	return file_proto_lake_proto_rawDescData
}

var file_proto_lake_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_proto_lake_proto_msgTypes = make([]protoimpl.MessageInfo, 6)
var file_proto_lake_proto_goTypes = []interface{}{
	(SubscribeResType)(0), // 0: SubscribeResType
	(*PublishReq)(nil),    // 1: PublishReq
	(*PublishRes)(nil),    // 2: PublishRes
	(*SubscribeReq)(nil),  // 3: SubscribeReq
	(*SubscribeRes)(nil),  // 4: SubscribeRes
	(*MsgCapsule)(nil),    // 5: MsgCapsule
	(*Signature)(nil),     // 6: Signature
}
var file_proto_lake_proto_depIdxs = []int32{
	5, // 0: PublishReq.msg_capsule:type_name -> MsgCapsule
	5, // 1: SubscribeReq.msg_capsule:type_name -> MsgCapsule
	0, // 2: SubscribeRes.type:type_name -> SubscribeResType
	5, // 3: SubscribeRes.msg_capsule:type_name -> MsgCapsule
	6, // 4: MsgCapsule.signature:type_name -> Signature
	1, // 5: Lake.Publish:input_type -> PublishReq
	3, // 6: Lake.Subscribe:input_type -> SubscribeReq
	2, // 7: Lake.Publish:output_type -> PublishRes
	4, // 8: Lake.Subscribe:output_type -> SubscribeRes
	7, // [7:9] is the sub-list for method output_type
	5, // [5:7] is the sub-list for method input_type
	5, // [5:5] is the sub-list for extension type_name
	5, // [5:5] is the sub-list for extension extendee
	0, // [0:5] is the sub-list for field type_name
}

func init() { file_proto_lake_proto_init() }
func file_proto_lake_proto_init() {
	if File_proto_lake_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_proto_lake_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*PublishReq); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_proto_lake_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*PublishRes); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_proto_lake_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SubscribeReq); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_proto_lake_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SubscribeRes); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_proto_lake_proto_msgTypes[4].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MsgCapsule); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_proto_lake_proto_msgTypes[5].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Signature); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	file_proto_lake_proto_msgTypes[3].OneofWrappers = []interface{}{
		(*SubscribeRes_Ok)(nil),
		(*SubscribeRes_MsgCapsule)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_proto_lake_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   6,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_proto_lake_proto_goTypes,
		DependencyIndexes: file_proto_lake_proto_depIdxs,
		EnumInfos:         file_proto_lake_proto_enumTypes,
		MessageInfos:      file_proto_lake_proto_msgTypes,
	}.Build()
	File_proto_lake_proto = out.File
	file_proto_lake_proto_rawDesc = nil
	file_proto_lake_proto_goTypes = nil
	file_proto_lake_proto_depIdxs = nil
}
