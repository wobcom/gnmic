// Copyright © 2020 Karim Radhouani <medkarimrdi@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"

	nokiasros "github.com/karimra/sros-dialout"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// listenCmd represents the listen command
var listenCmd = &cobra.Command{
	Use:   "listen",
	Short: "listens for telemetry dialout updates from the node",

	RunE: func(cmd *cobra.Command, args []string) error {
		server := new(dialoutTelemetryServer)
		address := viper.GetStringSlice("address")
		if len(address) == 0 {
			return fmt.Errorf("no address specified")
		}
		if len(address) > 1 {
			fmt.Printf("multiple addresses specified, listening only on %s\n", address[0])
		}
		var err error
		server.listener, err = net.Listen("tcp", address[0])
		if err != nil {
			return err
		}
		logger.Printf("waiting for connections on %s", address[0])
		var opts []grpc.ServerOption
		if viper.GetInt("max-msg-size") > 0 {
			opts = append(opts, grpc.MaxRecvMsgSize(viper.GetInt("max-msg-size")))
		}
		opts = append(opts, grpc.MaxConcurrentStreams(viper.GetUint32("max-concurrent-streams")))
		if viper.GetString("tls-key") != "" && viper.GetString("tls-cert") != "" {
			tlsConfig := &tls.Config{
				Renegotiation:      tls.RenegotiateNever,
				InsecureSkipVerify: viper.GetBool("skip-verify"),
			}
			err := loadCerts(tlsConfig)
			if err != nil {
				logger.Printf("failed loading certificates: %v", err)
			}

			err = loadCACerts(tlsConfig)
			if err != nil {
				logger.Printf("failed loading CA certificates: %v", err)
			}
			opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
		}

		server.grpcServer = grpc.NewServer(opts...)
		nokiasros.RegisterDialoutTelemetryServer(server.grpcServer, server)

		server.grpcServer.Serve(server.listener)
		defer server.grpcServer.Stop()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listenCmd)

	listenCmd.Flags().Uint32P("max-concurrent-streams", "", 256, "max concurrent streams gnmiClient can receive per transport")

	viper.BindPFlag("listen-max-concurrent-streams", listenCmd.LocalFlags().Lookup("max-concurrent-streams"))
}

type dialoutTelemetryServer struct {
	listener   net.Listener
	grpcServer *grpc.Server
}

func (s *dialoutTelemetryServer) Publish(stream nokiasros.DialoutTelemetry_PublishServer) error {
	peer, ok := peer.FromContext(stream.Context())
	if ok && viper.GetBool("debug") {
		b, err := json.Marshal(peer)
		if err != nil {
			logger.Printf("failed to marshal peer data: %v", err)
		} else {
			logger.Printf("received Publish RPC from peer=%s", string(b))
		}
	}
	md, ok := metadata.FromIncomingContext(stream.Context())
	if ok && viper.GetBool("debug") {
		b, err := json.Marshal(md)
		if err != nil {
			logger.Printf("failed to marshal context metadata: %v", err)
		} else {
			logger.Printf("received http2_header=%s", string(b))
		}
	}
	meta := make(map[string]interface{})
	if sn, ok := md["subscription-name"]; ok {
		if len(sn) > 0 {
			meta["subscription-name"] = sn[0]
		}
	} else {
		logger.Println("could not find subscription-name in http2 headers")
	}
	meta["source"] = peer.Addr.String()
	if systemName, ok := md["system-name"]; ok {
		if len(systemName) > 0 {
			meta["system-name"] = systemName[0]
		}
	} else {
		logger.Println("could not find system-name in http2 headers")
	}
	lock := new(sync.Mutex)
	for {
		subResp, err := stream.Recv()
		if err != nil {
			if err != io.EOF {
				logger.Printf("gRPC dialout receive error: %v", err)
			}
			break
		}
		err = stream.Send(&nokiasros.PublishResponse{})
		if err != nil {
			logger.Printf("error sending publish response to server: %v", err)
		}
		lock.Lock()
		printSubscribeResponse(meta, subResp)
		lock.Unlock()
	}
	return nil
}
