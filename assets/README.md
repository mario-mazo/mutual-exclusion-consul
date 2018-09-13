# Mutually exclusive workers using golang and consul

Implementing mutually exclusive is fun and useful. In this post I will show how to do a naive implementation (DO NOT use this in production) of this
pattern using client side [leader election](https://www.consul.io/docs/guides/leader-election.html) with consul.

The reason I chose consul for this task is because it was already available in our infrastructure, but the same could be achieved with different
tools, for example `AWS dynamoDB`. Basically you just need a `Key-Value` store that support locks.

All the code snippets in the post are taken from a [sample github repo][github-repo] where you can find the full implementation.

## The problem

We had a `job` server that each hour would connect to a postgreSQL database read some tasks
then it would proceed the execute the tasks created in the last hour, and then it would write back
to the database if the task was succefully finished.

The problem with this is that it doesn't scale. The jobs began to take longer and longer and if machine crashed all tasks were
not executed

## The solution - Mutually exclusive workers (Distributed Locks)

We knew we needed to run multiple instances of the job server. But each task could only be executed once. So we
decided to use `distributed locks` with `consul`. After a quick search we realized that implenting the [leader election](https://www.consul.io/docs/guides/leader-election.html)
_algorithm_ was the best solution for us.

So the whole concept is quite simple. All job servers get a list of tasks to be executed, then they iterate over the tasks list, get a `lock` on the task
so all other job servers skip that task, and move on to the next task. This way we can have multiple nodes working at the same all working on different tasks.

![Architecture][arch]

### Client side leader election

The [leader election](https://www.consul.io/docs/guides/leader-election.html) _algorithm_ is quite simple its just two steps:

- Create a session in consul
- Try to put a `Lock` on that session. If you succeed you are `leader` if not... well you are not `leader`

#### *Step 1:* Creating the session

First we will create a small wrapper function around the session creation. So
we always create a session no matter if we can work on a specific task or not.

```go
func (ec *exclusiveWorker) createSession() error {
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
```

We will pass to configuration parameters to the [go consul client](https://github.com/hashicorp/consul/api)

- `TTL`: This is the time out for the session. After this time has passed, consul will execute the _behavior_. I have set for this example

- `Behavior`: This `delete` behavior means that after the TTL has been reached the session is deleted and the Key associated with it

If you want to better understand how sessions work please read the official [consul sessions][consul-sessions] documentation. But the main take is this:

> If the release behavior is being used, any of the locks held in association with the session are released, and the ModifyIndex of the key is incremented. Alternatively, if the delete behavior is used, the key corresponding to any of the held locks is simply deleted. This can be used to create ephemeral entries that are automatically deleted by Consul.

#### *Step 2:* Acquire the session

Once we have a session we try to aquire the session. If we succed put a `Lock` on it and return _success_. We are leaders. If we failt to
acquire the lock it means that the _task_ is already being executed by another server. Here is were the mutual exlusitivy happens.

Again we are going to wrap this in a simple funcion for easy handling

```go
func (ec *exclusiveWorker) acquireSession() (bool, error) {
	KVpair := &api.KVPair{
		Key:     ec.key,
		Value:   []byte(ec.sessionID),
		Session: ec.sessionID,
	}

	aquired, _, err := ec.client.KV().Acquire(KVpair, nil)
	return aquired, err
}
```

We are going to need to pass 3 values to the consul client

- `Key`: This is the indentifier of the tasks. To follow conventions I chose `service/<TASK_NAME>/leader` but could be anything that better fit your needs. I really thought about using `service/<APP_NAME>/<TASK_NAME>`.
- `Value`: This is really unimportant when you are using consul for distributed locks. I chose the current `sessionID` (which is nothing but an UUID) for easy debugging. But could well be `rambo` or `goku`
- `Session`: is the ID of the session we are going to try to _Lock_

### *Step 3:* 

There is no step 3. That's all is there for the leader election part. But we still need a couple of helper functions we are going to need to make this work in a more realistic way.

#### Destroy session

Once our task is done we have to nice and release the lock. We could wait for the `TTL` and `beahvior` to kick in, but that's not nice. So lets implement
a basic destroy session function we can call when our task is finished, and all we need is the sessionID.

```go
func (ec *exclusiveWorker) destroySession() error {
	_, err := ec.client.Session().Destroy(ec.sessionID, nil)
	if err != nil {
		erroMsg := fmt.Sprintf("ERROR cannot delete key %s: %s", ec.key, err)
		return errors.New(erroMsg)
	}

	return nil
}
```

#### Renew session
If the tasks is taking more than the `TTL` the session and key are deleted by the `behavior`. If this happens a different server could lock exactly the same task and you would execute 2 times the same task which is what we are trying to avoid.

So we need to renew the session constanly to avoid triggering the `behavior`. The consul client comes with a handy function `RenewPeriodic()` that does exactly that. So write the wrapper:

```go
func (ec *exclusiveWorker) renewSession(doneChan <-chan struct{}) error {
	err := ec.client.Session().RenewPeriodic(ec.sessionTimeout, ec.sessionID, nil, doneChan)
	if err != nil {
		return err
	}
	return nil
}
```

 Here we need 3 things:

 - `sessionTimeout`: This is the original `TTL`. The client will use this to refres the session each `TTL/2`
 - `sessionID`: Id of the session we want to renew
 - `doneChan`: Is channel we use the signal that we need to keep renewing the session or if we close the channel we mean that we are done with the task and we don't need to renew the session anymore

## Demo

In the accompanying [github repo][github-repo] There is a fully working implementation of this. It's also very simple and intended for learning purporses.
You will need a work go installation to be able to compile the code and `docker` with `docker-compose` to be able to run a consul server. So lets see the demo:
First launch consul

```sh
$ docker-compose up
```

Then open 2 terminals and in the first run the code and you should see something like this:

```sh
$ go run main.go
sessionID: bac7cf19-285e-9907-98ad-e8189a07cbd9
I can work. YAY!!!
Starting to work
```

you now can check the web interface of consul [http://localhost:8500/ui/dc1/kv](http://localhost:8500/ui/dc1/kv)
to verify that the keys are created, locked and destroyed either by `TTL`, finishing the task or interrupting the task.

in the second one if you run the code you can see the code exiting while the task is executed

```sh
$ go run main.go
sessionID: d0f26b95-11cb-236c-bba7-601441f2ae74
I can NOT work. YAY!!!
$
```

if you interrupt the task by doing `Ctrl+C` you should see the cleanup happening

```sh
$ go run main.go
sessionID: 4685d391-251d-9f6d-1c2c-5ab6fdbd9f98
I can work. YAY!!!
Starting to work
^C2018/09/13 11:16:04 Job interrupted. Cleaning up
```

if you try to connect right after the task is finised you will notice you cannot conect. This is due to `lock-delay` which is documented in the [sessions][consul-sessions] section of the consul documentation.

## Final thoughts

I invite you to check the [github repo][github-repo] as the code there is full of notes about implementation that could be useful for a real implementation.

Also there are things I did not implement like `stop()` or `Discovering the Leader`, but implementation should be simple. Feel free to submit a pull requres.

## Links

- [https://www.consul.io/docs/guides/leader-election.html](https://www.consul.io/docs/guides/leader-election.html)
- [https://www.consul.io/docs/internals/sessions.html](https://www.consul.io/docs/internals/sessions.html)
- [https://github.com/mario-mazo/mutual-exclusion-consul](https://github.com/mario-mazo/mutual-exclusion-consul)

[arch]: https://raw.githubusercontent.com/mario-mazo/mutual-exclusion-consul/master/assets/mutual-exclusion.jpg "architecture"

[consul-sessions]: https://www.consul.io/docs/internals/sessions.html "sessions"

[github-repo]: https://github.com/mario-mazo/mutual-exclusion-consul "sample project"