package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/IBM/sarama"
	"github.com/armon/circbuf"

	dkplugin "github.com/distribworks/dkron/v4/plugin"
	dktypes "github.com/distribworks/dkron/v4/types"
)

const maxBufSize = 500000

type Kafka struct{}

func (s *Kafka) Execute(args *dktypes.ExecuteRequest, cb dkplugin.StatusHelper) (*dktypes.ExecuteResponse, error) {
	out, err := s.ExecuteImpl(args)
	resp := &dktypes.ExecuteResponse{Output: out}
	if err != nil {
		resp.Error = err.Error()
	}
	return resp, nil
}

func createTLSConfigFromContent(caCertContent, certContent, keyContent string) (*tls.Config, error) {
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM([]byte(caCertContent)) {
		return nil, errors.New("failed to append CA cert")
	}

	clientCert, err := tls.X509KeyPair([]byte(certContent), []byte(keyContent))
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert and key: %w", err)
	}

	return &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{clientCert},
	}, nil
}

func (s *Kafka) ExecuteImpl(args *dktypes.ExecuteRequest) ([]byte, error) {
	output, err := circbuf.NewBuffer(maxBufSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create circular buffer: %w", err)
	}

	debug := args.Config["debug"] == "true"

	brokerAddress := args.Config["brokerAddress"]
	topic := args.Config["topic"]
	message := args.Config["message"]
	if brokerAddress == "" {
		return output.Bytes(), errors.New("brokerAddress is empty")
	}
	if topic == "" {
		return output.Bytes(), errors.New("topic is empty")
	}

	// TLS Configuration
	caCert := args.Config["sslCaCert"]
	userCert := args.Config["sslUserCert"]
	userKey := args.Config["sslAccessKey"]

	tlsConfig, err := createTLSConfigFromContent(caCert, userCert, userKey)
	if err != nil {
		return output.Bytes(), fmt.Errorf("failed to create TLS config: %w", err)
	}

	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Retry.Max = 5
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	config.Net.TLS.Enable = true
	config.Net.TLS.Config = tlsConfig

	brokers := strings.Split(brokerAddress, ",")
	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		if debug {
			log.Printf("sarama config: %#v\n", config)
		}
		return output.Bytes(), fmt.Errorf("failed to create Kafka producer: %w", err)
	}
	defer producer.Close()

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(message),
	}

	_, _, err = producer.SendMessage(msg)
	if err != nil {
		return output.Bytes(), fmt.Errorf("failed to send message: %w", err)
	}

	output.Write([]byte("Result: successfully produced the message on Kafka broker\n"))
	return output.Bytes(), nil
}
