package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/consul/api"
)

type exclusiveWorkerConfig struct {
	client         *api.Client // Consul client
	key            string      // Worker Key
	sessionTimeout string      // Session timeout
}

type exclusiveWorker struct {
	client         *api.Client // Consul client
	key            string      // Worker Key
	sessionID      string      // Id of session
	sessionTimeout string      // Session timeout
}

func newExclusiveWorker(ewc *exclusiveWorkerConfig) *exclusiveWorker {
	ew := &exclusiveWorker{
		client:         ewc.client,
		key:            ewc.key,
		sessionTimeout: ewc.sessionTimeout,
	}
	return ew
}

// Step1: Create session
func (ec *exclusiveWorker) createSession() error {

	// you can call session.Destroy on the old session ID
	// that has acquired the Key (which you can find by calling kv.Get on your key).
	// This will cause the session behavior to trigger - e.g. if the behavior is set to delete,
	// the key will be deleted. This is the same as the session expiring normally.
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

//step2: Acquire Session
func (ec *exclusiveWorker) acquireSession() (bool, error) {
	KVpair := &api.KVPair{
		Key:     ec.key,
		Value:   []byte(ec.sessionID),
		Session: ec.sessionID,
	}

	aquired, _, err := ec.client.KV().Acquire(KVpair, nil)
	return aquired, err
}

// we need to renew the session bc the TTL will kill
// the session if its not renewed and the task is taking too long
func (ec *exclusiveWorker) renewSession(doneChan chan struct{}) error {
	err := ec.client.Session().RenewPeriodic(ec.sessionTimeout, ec.sessionID, nil, doneChan)
	if err != nil {
		return err
	}
	return nil
}

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

	if canWork {
		fmt.Println("I can work yay")

		doneChan := make(chan struct{})
		go w.renewSession(doneChan)
		fmt.Println("Starting to work")
		time.Sleep(30 * time.Second)
		close(doneChan) // +TLL
		fmt.Println("Work done")
		w.destroySession()
		return
	}

	fmt.Println("I can NOT work yay")
}
