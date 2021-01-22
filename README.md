# volcano-study

This is a repo for volcano code base study.


###Action Pipeline

Volcano:  
Enqueue -> Allocate -> Backfill

Kube-Batch:  
Allocate -> Backfill -> Preempt -> Reclaim


## Scheduler 入口
[scheduler.go](https://github.com/volcano-sh/volcano/blob/c688677d9e8525ebc242a0527927bfb954d1a414/pkg/scheduler/scheduler.go#L39)

Q: What does it take to assemble a volcano scheduler?
A: Basically: a cache, a set of actions, a set of plugins to carry out those actions.

1. Run   
load configuration -> run cache ->  for {runOnce/1秒}。

2. RunOnce   
每一次runOnce会开一个session。session里面有plugin的信息。每个[]action依次execute，需要依赖session的信息。

``` go
	actions := pc.actions
	plugins := pc.plugins
	configurations := pc.configurations
	pc.mutex.Unlock()

	ssn := framework.OpenSession(pc.cache, plugins, configurations)
	defer framework.CloseSession(ssn)

	for _, action := range actions {
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
	}
	metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))

```


3. watchSchedulerConf   
还可以热修改scheduler的配置文件。所以```loadSchedulerConf```要加锁。


## Cache

疑问：
- RecordJobStatusEvent
- UpdateJobStatus. 介绍是puts job in backlog for a while. What is backlog?
- cache能返回一个clientset，以kubernetes.Interface的类型。
- 定义了Binder和Evictor来bind和evict. 这是什么设计模式。=> scheduleCache实现struct中包含binder和evictor结构来触发bind和evict。这样方便binder和evictor的可拓展性。
- updatepodcondition和updategroup/updateJobStatus。



1. Binder通过client-go的clientset发出bind请求。将一个pod安排给一个node。（cache是否会记录这一信息呢？是用statusupdater来完成吗？-> 答案：cache专门有方法来做这个事情）
https://github.com/volcano-sh/volcano/blob/0096919b5890206d9107a9cda93ed84ae7f99181/pkg/scheduler/cache/cache.go#L116
``` go
db.kubeclient.CoreV1().Pods(p.Namespace).Bind(context.TODO(),
		&v1.Binding{
			ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name, UID: p.UID, Annotations: p.Annotations},
			Target: v1.ObjectReference{
				Kind: "Node",
				Name: hostname,
			},
		},
		metav1.CreateOptions{})
```

2. Evict的时候先更新podCondition,再delete pod

``` go
if _, err := de.kubeclient.CoreV1().Pods(p.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Failed to update pod <%v/%v> status: %v", pod.Namespace, pod.Name, err)
		return err
	}
	if err := de.kubeclient.CoreV1().Pods(p.Namespace).Delete(context.TODO(), p.Name, metav1.DeleteOptions{}); err != nil {
		klog.Errorf("Failed to evict pod <%v/%v>: %#v", p.Namespace, p.Name, err)
		return err
    }
```

3. StatusUpdater  
    - 调用utilpod来update podcondition
    - updatepodgroup的过程比较曲折：```schedulingapi.PodGroup``` -> ```v1alpha1.PodGroup/vcv1beta1.PodGroup```。然后```vcclient.SchedulingV1beta1().PodGroups(podgroup.Namespace).Update```。更新之后拿回一个updated，```v1alpha.PodGroup``` -> ```api.PodGroup```.

4. Cache中的各种用作缓存的informer：  ```nodeInformer```,```pvcInformer```,```pvInformerscInformer```,```csiNodeInformer```,```pcInformer```,```quotaInformer```. 其中```nodeInformer```,```podInformer```，```pcInformer```，```quotaInformer```需要handler应对资源的变更。专门在event_handler文件中建立informer观测到的api server发生的事件与cache的对应关系。
```podInformer```需要用```FilteringResourceEventHandler```filter掉一些不该volcano-scheduler负责的pod。

volcano自己api的informer：```podGroupInformer```, ```queueInformerV1beta1```

5. cache的evict/bind方法  
线程安全的cache操作。cache evcit方法evict的是task。会在cache层面更新task/pod和node的状态。也会调用evictor的方法做实际的evict。不成功时（通常是在map中找不到task，会触发resyncTask）  
bind同理。

6. processCleanupJob   
消费被添加到workqueue```deletedJobs```中的jobs。将此job从map中删除。

7. processResyncTask   
消费被添加到errTasks中的task。

8. Snapshot   
返回一个反应当前集群信息的结构体。
``` go
snapshot := &schedulingapi.ClusterInfo{
    Nodes:         make(map[string]*schedulingapi.NodeInfo),
    Jobs:          make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
    Queues:        make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
    NamespaceInfo: make(map[schedulingapi.NamespaceName]*schedulingapi.NamespaceInfo),
}
```
每个jobs的信息采用多线程的方式clone加快速度。
会把整个clone一遍。

9. RecordJobStatusEvent  
基本上是调用```recordPodGroupEvent```。貌似是eventchan的生产者？

## Framework

Framework不出意外规定了一个调度周期的流程。看一个package先看看interface。

1. interface.go
定义了Action和Plugin两个interface(抽象类)。其中Action有三个动作：initilize, execute, uninitilize. 一个plugin需要具有两个动作OnSessionOpen和OnSessionClose.

2. framework.go   
framework.go以及framework包中都咩有framework结构体～ 但是是session的入口。  
    - framework.OpenSession(...) -> openSession(cache)
    - 先一个tier一个tier地把plugins build出来，存到session的一个map里面。然后每个plugins做OnSessionOpen的操作。
    - framework.CloseSession(...) -> closeSession(ssn)
    - 每个plugin执行OnSessionClose的操作。

疑问： 
- tier到底是啥？
实际上scheduler结构体中的plugins就是conf.Tier类型。一个tier包含一串plugins：
``` go
// Tier defines plugin tier
type Tier struct {
	Plugins []PluginOption `yaml:"plugins"`
}
```
配置文件长这样：
``` yaml
actions: "reclaim, allocate, backfill, preempt"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
```
那是怎样确定执行哪个tier的plugin呢？这个应该是在action里面的逻辑。因为此处framework只是在初始化每个plugin。具体的执行是由action完成。之后再看。

3. plugins.go   
    - framework包中的package文件主要是用于plugin，action的注册及建造。GetPluginBuilder是要根据plugin的name从pluginBuilders这个map中取出一个能用的pluginBuilder函数。那肯定就有将这些函数注册到这个map的一个步骤，也就是 RegisterPluginBuilder。这个register的步骤在plugins包中有体现。https://github.com/volcano-sh/volcano/blob/e4777f1751a15f07df7a3754b83a6e29e79880ef/pkg/scheduler/plugins/factory.go  
    - action的注册类似。
4. plugins.go 自定义的plugin较为特殊。   
这个要重点看一下，应为关系到实现lease term和多租户的自定义插件实现。  
插件应该是可以单独编译的。 LoadCustomPlugins会加载```*.so```文件，解析出一个pluginBuilder, 在将这个pluginBuilder注册到pluginBuildersz这个map里去。https://github.com/volcano-sh/volcano/blob/c688677d9e8525ebc242a0527927bfb954d1a414/pkg/scheduler/framework/plugins.go#L63 。loadPluginBuilder使用了go原生的plugin的包来```plug.Lookup("New")```找到New函数的symbol，再将这个symbol type-assertion一下```symBuilder.(PluginBuilder)```. 学到了。

plugins.go这个文件主要就是两个单例：
``` go 
// Plugin management
var pluginBuilders = map[string]PluginBuilder{}
// Action management
var actionMap = map[string]Action{}
```
来管理和实现plugin和action的拓展性。


5. session.go   
session是volcano重要的一个模块。一个session包含了一次调度所需要的所有信息。  
session记录的状态有：
``` go
	podGroupStatus map[api.JobID]scheduling.PodGroupStatus
	Jobs          map[api.JobID]*api.JobInfo
	Nodes         map[string]*api.NodeInfo
	Queues        map[api.QueueID]*api.QueueInfo
	NamespaceInfo map[api.NamespaceName]*api.
```
从cache.snapshot中来。每个job还要jobValid一下，目前不知道啥意思。
另外有一堆functions，看起来分为这几大类:
- api.CompareFn{} 
- api.PredicateFn{} 
- api.BestNodeFn{}
- api.BatchNodeOrderFn{}
- api.NodeMapFn{}
- api.NodeReduceFn{}
- api.EvictableFn{}
- api.ValidateFn{}
- api.ValidateExFn{}
- api.TargetJobFn{}
- api.ReservedNodesFn{}


每个plugins会在onSessionOpen时将填写session的这一堆fns。那么问题是这些plugins的onSessionOpen方法是什么时候被调用的呢？-> framework.OpenSession时每个plugin会调用各自的这个方法。
https://github.com/volcano-sh/volcano/blob/9f260d1bf455bb5caebc3eb3fb7a1e50c5aeb7c2/pkg/scheduler/framework/framework.go#L46

ssn中有一些方法，例如Pipeline, Allocate, Evict, UpdatePodGroupCondition. 做的事情都是在session里面改变api结构体的状态。在各个action中应该都会有调用。
dispatch负责与cache动作对接。  
这些方法涉及到三个地方状态的变化。1.首先ssn会改变自己的状态，2.调用cache.Bind时，cache会改变自己的状态。3.cahce.Bind时会调用client-go让api-server改变状态。



## Action

action包，即动作引擎，是volcano scheduler中最为重要的一个包。它直接决定了ssn中的状态该如何改变。  

volcano的action比kube-batch多了三个：
|volcano|kube-batch|
|-|-|
|allocate|allocate|
|backfill|backfill|
|preempt|preempt|
|reclaim|reclaim|
|enqueue||
|reserve||
|elect||

kube-batch scheduler默认的action为:
``` yaml
actions: "reclaim, allocate, backfill, preempt"
```
volcano scheduler默认的action为：
``` yaml
actions: "enqueue, allocate, backfill"
```
那么就先从这三个action开始看。

1. Enqueue

用的Fn有```ssn.QueueOrderFn```，```ssn.JobOrderFn```

Execute里有一个jobsMap:
``` go
jobsMap := map[api.QueueID]*util.PriorityQueue{}
```
以一个个priorityqueue的形式存每个job。每一个priorityqueue(即我们yaml中给一个job指定的queue)又以priorityqueue的方式排列（Q中Q).

Execute的第一段逻辑就是将ssn的所有状态为PodGroupPending的jobs以这种方式入队排列好。

Execute的第二段逻辑是将先计算出一个idle的资源量（api里的resource_info类型). 根据queue的次序和queue内job的次序遍历，如果idle资源能满足，将这个job的podgroup状态设置为PodGroupInqueue并在ssn中更新。（每次inqueue一个，idle资源要扣除一个）。注意在此处没有对任何全局的队列产生影响，只是将ssn中的jobs的podgroup状态改变为了PodGroupInqueue。所谓的priorityqueue只对遍历的顺序产生了影响。

2. Allocate
Allocate是调度的核心动作。代码注释中给出的调度逻辑为：
```
	// the allocation for pod may have many stages
	// 1. pick a namespace named N (using ssn.NamespaceOrderFn)
	// 2. pick a queue named Q from N (using ssn.QueueOrderFn)
	// 3. pick a job named J from Q (using ssn.JobOrderFn)
	// 4. pick a task T from J (using ssn.TaskOrderFn)
	// 5. use predicateFn to filter out node that T can not be allocated on.
	// 6. use ssn.NodeOrderFn to judge the best node and assign it to T

```
一共四个priorityqueue，namespace之间一个，queue之间一个，job之间一个，job里的task之间一个。每个都用一个fn来排序。

PodGroupPending的podgroup不会被考虑。对了，podgroup到底有几个状态？(A: ```PodGroupPending```,```PodGroupRunning```,```PodGroupUnknown```,```PodGroupInqueue```) plugin的JobValid没通过的也不行。

嵌套的map。表示一个job有两种multi-tenet属性，即namesapce和queue：
``` go
jobsMap := map[api.NamespaceName]map[api.QueueID]*util.PriorityQueue{}
```
但每个ns的queue会有重复（因为有job来自不同的ns，但是相同的queue名）。所以单个map[api.NamespaceName][QueueID]中的queue们的排序没有用pq来实现。因为此处希望各个ns中的queue的排序是独立的，不要互相干扰。

``` go
for queueID := range queueInNamespace {
    ....
    if queue == nil || ssn.QueueOrderFn(currentQueue, queue) {
        queue = currentQueue
    }
}
```

首先遍历每个queueInNamespace里的每个job。对于每个job，会新建一个task的pq。一个task一个task的遍历。

会调用redicateFn来predicate node

``` go
predicateNodes, fitErrors := util.PredicateNodes(task, nodes, predicateFn)
```
以此产生的candidate node又会被各种function排序
``` go
    nodeScores := util.PrioritizeNodes(task, candidateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)

    node := ssn.BestNodeFn(task, nodeScores)
    if node == nil {
        node = util.SelectBestNode(nodeScores)
    }
```

最终的绑定由statment来实现。statement将所有需要allocate的task收集好，最后统一commit。



一个疑问：每一个task在predicat的时候都遍历的全部的node，queue的资源分区是如何实现的？如何限制了每个task可见的node范围？是overusedFn吗?

3. Backfill
Backfill中目前只支持对没有指定资源数的opportunistic task进行的node绑定。其他case还在开发之中。
``` go 

if task.InitResreq.IsEmpty() {
 ....
 	for _, node := range ssn.Nodes {
		 ...

	 }
} else {
	// TODO (k82cn): backfill for other case.
}

```



## Plugins

重点关注多租户的plugin实现。

design doc中提到的关于多租户的：[queue.md](https://github.com/volcano-sh/volcano/blob/d791592e0051c76e39a259f23532bc35889d01d0/docs/design/queue/queue.md), [fairshare.md](https://github.com/volcano-sh/volcano/blob/d791592e0051c76e39a259f23532bc35889d01d0/docs/design/fairshare.md).

queue.md中提到了queue的share by weight功能是在propotion plugin中实现的。Propotion这个plugin在reclaim action中也有用到。文档中提到的Backfill中的```ignore deserved guarantee of queue to fill idle resources as much as possible.```并没有实现。


fairshare.md介绍的是同一个queue中不同user间的fair share问题。在valcano中，除了每个queue有一个weight，每一个ns的ResourceQuota中可以附加一个```volcano.sh/namespace.weight```的field。



参考资料
1. https://zoux86.github.io/post/2019-12-02-volcano%E7%AE%80%E4%BB%8B/
2. https://zoux86.github.io/post/2019-11-24-kube-batch-%E5%A6%82%E4%BD%95%E5%AE%9E%E7%8E%B0gang-scheduler/
3. https://zoux86.github.io/post/2019-12-02-volcano-scheduler%E4%BB%A3%E7%A0%81%E6%B5%81%E7%A8%8B%E5%9B%BE/
4. https://zoux86.github.io/post/2019-11-24-kube-batch-%E7%AE%80%E4%BB%8B/



var queue *api.QueueInfo
for queueID := range queueInNamespace {
	currentQueue := ssn.Queues[queueID]
	if ssn.Overused(currentQueue) {
		klog.V(3).Infof("Namespace <%s> Queue <%s> is overused, ignore it.", namespace, currentQueue.Name)
		delete(queueInNamespace, queueID)
		continue
	}

	if queue == nil || ssn.QueueOrderFn(currentQueue, queue) {
		queue = currentQueue
	}
}


