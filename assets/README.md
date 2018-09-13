# Mutually exclusive workers using golang and consul

Implementing mutually exclusive is fun and useful. In this post I will show how to do a naive implementation (DO NOT use this in production) of this
pattern using client side [leader election](https://www.consul.io/docs/guides/leader-election.html) with consul.

The reason I chose consul for this task is because it was already available in our infrastructure, but the same could be achieved with different
tools, for example `AWS dynamoDB`. Basically you just need a `Key-Value` store that support locks.

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

There is no step 3. That's all is there, but we still need a couple of helper functions we are going to need to make this work in a more realistic way.

#### Destroy session



#### Renew session


## Demo

## Links

- [https://www.consul.io/docs/guides/leader-election.html](https://www.consul.io/docs/guides/leader-election.html)

## TODOs

- Implement `stop()`
- Implement `Discovering the Leader`


[arch]: https://raw.githubusercontent.com/mario-mazo/mutual-exclusion-consul/master/assets/mutual-exclusion.jpg "Architecture"

[consul-sessions]: https://www.consul.io/docs/internals/sessions.html "sessions"