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
- Try to put a `Lock` on that session

#### *Step 1:* Creating the session
First we will create a small wrapper function around the session creation. 

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

- `TTL`: This is the time out for the session. After this time has passed, consul will execute the _behavior_

- `Behavior`: This `delete` behavior means that after the TTL has been reached the session is deleted and the Key associated with it

## Links

- [https://www.consul.io/docs/guides/leader-election.html](https://www.consul.io/docs/guides/leader-election.html)

## TODOs

- Implement `stop()`
- Implement `Discovering the Leader`


[arch]: https://raw.githubusercontent.com/mario-mazo/mutual-exclusion-consul/master/assets/mutual-exclusion.jpg "Architecture"

