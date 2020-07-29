package udp_output

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/karimra/gnmic/collector"
	"github.com/karimra/gnmic/outputs"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"
)

func init() {
	outputs.Register("udp", func() outputs.Output {
		return &UDPSock{
			Cfg: &Config{},
		}
	})
}

type UDPSock struct {
	Cfg      *Config
	ctx      context.Context
	cancelFn context.CancelFunc
	buffer   chan []byte
	limiter  *time.Ticker
	logger   *log.Logger
	mo       *collector.MarshalOptions
}

type Config struct {
	Address    string // ip:port
	Rate       time.Duration
	BufferSize uint
	Format     string
}

func (u *UDPSock) Init(cfg map[string]interface{}, logger *log.Logger) error {
	err := mapstructure.Decode(cfg, u.Cfg)
	if err != nil {
		return err
	}
	_, _, err = net.SplitHostPort(u.Cfg.Address)
	if err != nil {
		return fmt.Errorf("wrong address format: %v", err)
	}
	u.logger = log.New(os.Stderr, "udp_output ", log.LstdFlags|log.Lmicroseconds)
	if logger != nil {
		u.logger.SetOutput(logger.Writer())
		u.logger.SetFlags(logger.Flags())
	}
	u.buffer = make(chan []byte, u.Cfg.BufferSize)
	if u.Cfg.Rate > 0 {
		u.limiter = time.NewTicker(u.Cfg.Rate)
	}
	u.ctx, u.cancelFn = context.WithCancel(context.Background())
	u.mo = &collector.MarshalOptions{Format: u.Cfg.Format}
	go u.start()
	return nil
}
func (u *UDPSock) Write(m proto.Message, meta outputs.Meta) {
	if m == nil {
		return
	}
	b, err := u.mo.Marshal(m, meta)
	if err != nil {
		u.logger.Printf("failed marshaling proto msg: %v", err)
		return
	}
	u.buffer <- b
}
func (u *UDPSock) Close() error {
	u.cancelFn()
	u.limiter.Stop()
	close(u.buffer)
	return nil
}
func (u *UDPSock) Metrics() []prometheus.Collector { return nil }
func (u *UDPSock) String() string {
	b, err := json.Marshal(u)
	if err != nil {
		return ""
	}
	return string(b)
}

func (u *UDPSock) start() {
	var err error
	defer u.Close()
	for {
		select {
		case <-u.ctx.Done():
			return
		case b := <-u.buffer:
			if u.limiter != nil {
				<-u.limiter.C
			}
			err = u.send(b)
			if err != nil {
				u.logger.Printf("failed sending udp bytes: %v", err)
			}
		}
	}
}

func (u *UDPSock) send(b []byte) error {
	udpAddr, err := net.ResolveUDPAddr("udp", u.Cfg.Address)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return err
	}
	_, err = conn.Write(b)
	if err != nil {
		return err
	}
	return nil
}
