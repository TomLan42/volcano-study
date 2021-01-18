# volcano-study

This is a repo for volcano code base study.


###Action Pipeline

Volcano:  
Enqueue -> Allocate -> Backfill

Kube-Batch:  
Allocate -> Backfill -> Preempt -> Reclaim




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








