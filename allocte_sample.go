package main

import ("fmt")


namespaces := util.NewPriorityQueue(ssn.NamespaceOrderFn)
jobsMap := map[api.NamespaceName]map[api.QueueID]*util.PriorityQueue{}
for _, job := range ssn.Jobs {
	...
	namespace := api.NamespaceName(job.Namespace)
	queueMap, found := jobsMap[namespace]
	if !found {
		namespaces.Push(namespace)

		queueMap = make(map[api.QueueID]*util.PriorityQueue)
		jobsMap[namespace] = queueMap
	}
	jobs, found := queueMap[job.Queue]
	if !found {
		jobs = util.NewPriorityQueue(ssn.JobOrderFn)
		queueMap[job.Queue] = jobs
	}
}
pendingTasks := map[api.JobID]*util.PriorityQueue{}
...



for {
	if namespaces.Empty() {
		break
	}
	// pick namespace from namespaces PriorityQueue
	namespace := namespaces.Pop().(api.NamespaceName)
	queueInNamespace := jobsMap[namespace]
	// pick queue for given namespace
	var queue *api.QueueInfo
	queue = ...
	// pick job for given queue
	jobs, found := queueInNamespace[queue.UID]
	job := jobs.Pop().(*api.JobInfo)
	// schedule task 
	tasks := pendingTasks[job.UID]
	for !tasks.Empty() {
		task := tasks.Pop().(*api.TaskInfo)
		...
	}
	// commit with Statement
	if ssn.JobReady(job) {
		stmt.Commit()
	}
	// Added Namespace back until no job in Namespace.
	namespaces.Push(namespace)
}



func loadPluginBuilder(pluginPath string) (PluginBuilder, error) {
	plug, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, err
	}
	symBuilder, err := plug.Lookup("New")
	if err != nil {
		return nil, err
	}
	builder, ok := symBuilder.(PluginBuilder)
	if !ok {
		return nil, fmt.Errorf("unexpected plugin: %s, failed to convert PluginBuilder `New`", pluginPath)
	}
	return builder, nil
}


type QueueInfo struct {
	UID  QueueID
	Name string

	Weight int32

	// Weights is a list of slash sperated float numbers.
	// Each of them is a weight corresponding the
	// hierarchy level.
	Weights string
	// Hierarchy is a list of node name along the
	// path from the root to the node itself.
	Hierarchy string

	Queue *scheduling.Queue
}


ssn.AddQueueOrderFn(pp.Name(), func(l, r interface{}) int {
	...
	if pp.queueOpts[lv.UID].share < pp.queueOpts[rv.UID].share {
		return -1
	}
	return 1
})



func (pp *proportionPlugin) updateShare(attr *queueAttr) {
	res := float64(0)
	for _, rn := range attr.deserved.ResourceNames() {
		share := helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn))
		if share > res {
			res = share
		}
	}
	attr.share = res
	metrics.UpdateQueueShare(attr.name, attr.share)
}



ssn.AddEventHandler(&framework.EventHandler{
	AllocateFunc: func(event *framework.Event) {
		...
		pp.updateShare(attr)
	},
	DeallocateFunc: func(event *framework.Event) {
		...
		pp.updateShare(attr)
	},
})


