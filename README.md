# Message Lake 📮

Message Lake (msg-lake) is an open-source message exchanging platform designed
to provide a simple and scalable solution for message-based communication
between clients. It aims to provide an environment where clients can become or
work as servers with a publish-subscribe model, enabling flexible and dynamic
communication patterns.

## Features

- Publish-Subscribe Model: Supports a publish-subscribe pattern for message
exchange, allowing publishers to send messages to specific topics, and
subscribers to receive messages from those topics.
- Scalability: Designed to scale horizontally to handle high message volumes and
accommodate growing demands.
- Extensibility: Provides a flexible and extensible message protocol that can be
easily integrated into existing systems.
- Efficiency: Optimized for efficient communication with minimal overhead.
- Interoperability: Supports multiple programming languages and platforms
through Protocol Buffers (protobuf) for message serialization.
- Cluster: Supports clustering of Message Lake agents to achieve high
availability and fault tolerance.

## Signed message protocol

msg-lake is an open relay: any client with a valid asymmetric key may publish
to or subscribe from any topic. It does not provide topic ACLs, nonce tracking,
replay rejection, or exactly-once delivery.

Publishers sign the deterministic Protocol Buffers encoding of `MsgCapsule`,
which binds `topic_id`, `data`, and `is_encrypted` to the client signature.
Subscribers sign the UTF-8 bytes of `topic_id`. The ingress agent verifies the
client signature and adds a nanosecond Unix timestamp outside the signed
capsule before relaying it through GossipSub. Every receiving agent verifies
the client signature and topic binding before local fan-out.

The Go client can additionally verify relayed messages before invoking the
subscription callback by passing `client.WithReceivedMessageVerification(true)`
to `client.NewClient`. This end-to-end client verification is disabled by
default to avoid an ECDSA verification for every subscriber delivery.

## Getting Started

To get started with Message Lake, follow these steps:

1. Deploy a msg-lake agent with `docker` command:
```shell
docker run --network "host" --rm ghcr.io/h0n9/msg-lake:latest agent
```

Alternatively, you can use the docker-compose command to deploy a cluster of
msg-lake agents:
```shell
docker-compose up
```

Make sure you have docker-compose installed if you choose to use the command
above.

2. Open a new terminal session and connect to msg-lake agent using the `docker`
command:
```shell
docker run --network "host" --rm -it ghcr.io/h0n9/msg-lake:latest client --topic "life-is-beautiful" --nickname "h0n9"
```

Feel free to enhance and customize the deployment instructions based on your
specific setup.
