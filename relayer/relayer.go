// Copyright 2021 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package relayer

import (
	"context"
	"fmt"

	"github.com/nonceblox/chainsafe-core/relayer/message"
	"github.com/rs/zerolog/log"
)

type Metrics interface {
	TrackDepositMessage(m *message.Message)
}

type RelayedChain interface {
	PollEvents(ctx context.Context, sysErr chan<- error, msgChan chan *message.Message)
	Write(message *message.Message) error
	DomainID() uint8
	CheckFeeClaim() bool
	GetFeeClaim(msg *message.Message) error
}

func NewRelayer(chains []RelayedChain, metrics Metrics, messageProcessors ...message.MessageProcessor) *Relayer {
	return &Relayer{relayedChains: chains, messageProcessors: messageProcessors, metrics: metrics}
}

type Relayer struct {
	metrics           Metrics
	relayedChains     []RelayedChain
	registry          map[uint8]RelayedChain
	messageProcessors []message.MessageProcessor
}

// Start function starts the relayer. Relayer routine is starting all the chains
// and passing them with a channel that accepts unified cross chain message format
func (r *Relayer) Start(ctx context.Context, sysErr chan error) {
	log.Debug().Msgf("Starting relayer")

	messagesChannel := make(chan *message.Message)
	for _, c := range r.relayedChains {
		log.Debug().Msgf("Starting chain %v", c.DomainID())
		r.addRelayedChain(c)
		go c.PollEvents(ctx, sysErr, messagesChannel)
	}

	for {
		select {
		case m := <-messagesChannel:
			go r.route(m)
			continue

		case <-ctx.Done():
			return

		}
	}

}

// Route function winds destination writer by mapping DestinationID from message to registered writer.
func (r *Relayer) route(m *message.Message) {
	r.metrics.TrackDepositMessage(m)

	destChain, ok := r.registry[m.Destination]

	if !ok {
		log.Error().Msgf("no resolver for destID %v to send message registered", m.Destination)
		return
	}
	sorcChain, ok := r.registry[m.Source]
	if !ok {
		log.Error().Msgf("no resolver for destID %v to send message registered", m.Source)
		return
	}

	if !ok {
		log.Error().Msgf("no resolver for destID %v to send message registered", m.Destination)
		return
	}

	for _, mp := range r.messageProcessors {
		if err := mp(m); err != nil {
			log.Error().Err(fmt.Errorf("error %w processing mesage %v", err, m))
			return
		}
	}

	log.Debug().Msgf("Sending message %+v to destination %v", m, m.Destination)
	// // fee method here.
	boolVal := destChain.CheckFeeClaim()
	if boolVal {
		if err := sorcChain.GetFeeClaim(m); err != nil {
			log.Error().Msgf("Claiming fees Error %+w", err)
			return
		}
	}
	if err := destChain.Write(m); err != nil {
		log.Error().Err(err).Msgf("writing message %+v", m)
		return
	}
}

func (r *Relayer) addRelayedChain(c RelayedChain) {
	if r.registry == nil {
		r.registry = make(map[uint8]RelayedChain)
	}
	domainID := c.DomainID()
	r.registry[domainID] = c
}
