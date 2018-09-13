package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/hashicorp/consul/api"
)

// exclusiveWorkerConfig holds the configuration to create a new Exclusive Worker
type exclusiveWorkerConfig struct {
	client         *api.Client // Consul client
	key            string      // Worker Key (in other words taskID)
	sessionTimeout string      // Session timeout
}

// exclusiveWorker is the struct that hold the worker (or Leader)
type exclusiveWorker struct {
	client         *api.Client // Consul client
	key            string      // Worker Key (in other words taskID)
	sessionID      string      // Id of session created in consul
	sessionTimeout string      // Session timeout
}

// newExclusiveWorker creates new exclusive worker
func newExclusiveWorker(ewc *exclusiveWorkerConfig) *exclusiveWorker {
	ew := &exclusiveWorker{
		client:         ewc.client,
		key:            ewc.key,
		sessionTimeout: ewc.sessionTimeout,
	}
	return ew
}

// Step1: Create session
// createSession creates a session in consul with especified TTL and behavior set to delete
func (ec *exclusiveWorker) createSession() error {
	// You can call session.Destroy on the old session ID
	// that has acquired the Key. This will cause the session behavior to trigger - e.g.
	// if the behavior is set to delete the key will be deleted.
	// This is the same as the session expiring normally.
	sessinConf := &api.SessionEntry{
		TTL:      ec.sessionTimeout,
		Behavior: "delete",
	}

	sessionID, _, err := ec.client.Session().Create(sessinConf, nil)
	if err != nil {
		return err
	}

	fmt.Println("sessionID:", sessionID)
	ec.sessionID = sessionID
	return nil
}

// step2: Acquire Session
// acquireSession basically creates the mutual exclusion lock
func (ec *exclusiveWorker) acquireSession() (bool, error) {
	KVpair := &api.KVPair{
		Key:     ec.key,
		Value:   []byte(ec.sessionID),
		Session: ec.sessionID,
	}

	aquired, _, err := ec.client.KV().Acquire(KVpair, nil)
	return aquired, err
}

// We need to renew the session because the TTL will destroy
// the session if its not renewed and the task is taking too long
// RenewPeriodic renews the session each sessionTimeout/2 as indicated in the code of the client.
// https://github.com/hashicorp/consul/blob/e3cabb3a261d9583393aec99ef50bbfc666128b9/api/session.go#L148
// renewSession takes a channel that we later use (by closing it) to signal that no more renewals are necessary
func (ec *exclusiveWorker) renewSession(doneChan <-chan struct{}) error {
	err := ec.client.Session().RenewPeriodic(ec.sessionTimeout, ec.sessionID, nil, doneChan)
	if err != nil {
		return err
	}
	return nil
}

// destroySession destroys the session by triggering the behavior. So it will delete de Key as well
func (ec *exclusiveWorker) destroySession() error {
	_, err := ec.client.Session().Destroy(ec.sessionID, nil)
	if err != nil {
		erroMsg := fmt.Sprintf("ERROR cannot delete key %s: %s", ec.key, err)
		return errors.New(erroMsg)
	}

	return nil
}

func main() {

	client, err := api.NewClient(&api.Config{Address: "localhost:8500"})
	if err != nil {
		log.Fatalln(err)
	}

	workerConf := &exclusiveWorkerConfig{
		client:         client,
		key:            "service/bobruner/leader",
		sessionTimeout: "15s",
	}

	w := newExclusiveWorker(workerConf)
	w.createSession()
	if err != nil {
		log.Fatalln(err)
	}
	defer w.destroySession()

	canWork, err := w.acquireSession()
	if err != nil {
		log.Fatalln(err)
	}

	// We handle the signal interrupt in case the job is interrupted  by
	// doing a Ctrl+C  in the terminal.
	// This can also be seen on how to stop the task which was not implemented
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		log.Println("Job interrupted. Cleaning up")
		err := w.destroySession()
		if err != nil {
			log.Println("Could not destroy session")
		}
		os.Exit(0)
	}()

	// If we were able to lock the session that means we are leaders so we can start
	// doing some work
	if canWork {
		fmt.Println("I can work. YAY!!!")

		doneChan := make(chan struct{})
		go w.renewSession(doneChan) // We send renewSession() to its own go routine

		// Here we simulate the long running task
		fmt.Println("Starting to work")
		time.Sleep(30 * time.Second)
		close(doneChan)
		fmt.Println("Work done")

		// Note: Due to lock-delay (default 15s) you will not be able to get
		//       the lock right after destroying the session
		//       https://www.consul.io/docs/internals/sessions.html
		err := w.destroySession()
		if err != nil {
			log.Println("Could not destroy session")
		}
		return
	}

	fmt.Println("I can NOT work. YAY!!!")
}
