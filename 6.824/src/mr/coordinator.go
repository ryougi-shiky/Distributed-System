package mr

import (
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

/*
In the coordinator, there are 4 conditions that require multiple threads to synchronize:

 1. When call FetchTask, if can't get task, need to check the state of the taskset and state of the coordinator.

 2. When call SubmitTask, need to update the state of the taskset and state of the task.

 3. When call timeoutDetector(), it continuously check if there are timeout tasks,
    if found, reassign the task, insert it to the queue.

 4. When call Done(), check coordinator's state to decide if the job is finished.

    Due to the above 4 conditions, we need to use mutex when accessing taskset and coordinator's state.
*/

type CoordinatorStatus int

const (
	MapStage CoordinatorStatus = iota
	ReduceStage
	FinishedStage
)

const IntermediateNameTemplate string = "mr-tmp-%d-%d"
const ChannelSize int = 10
const TimeoutCheckInterval time.Duration = 2

// The wait time to receive next task.
const WaitInterval time.Duration = 2

type Coordinator struct {
	// Your definitions here.
	Id               uuid.UUID
	Mutex            sync.Mutex
	CoordinatorStage CoordinatorStatus
	MapperTaskNum    int
	ReducerTaskNum   int
	TaskChannel      chan *Task
	TaskSet          *TaskSet
	Files            []string
}

// Your code here -- RPC handlers for the worker to call.
func (c *Coordinator) registerMapTasks() {
	for index, file := range c.Files {
		task := NewTask(MapTask, index, c.ReducerTaskNum, []string{file})
		c.TaskSet.RegisterTask(task)
		go func() { c.TaskChannel <- task }()
	}
	// log.Println("[Coordinator] Successfully registered map tasks.")
}

func (c *Coordinator) checkStageComplete() bool {
	switch c.CoordinatorStage {
	case MapStage:
		return c.TaskSet.CheckComplete(MapTask)
	case ReduceStage:
		return c.TaskSet.CheckComplete(ReduceTask)
	case FinishedStage:
		return true
	default:
		// log.Panic("Cannot check unsupported coordinator stage type.")
		return false
	}
}

func (c *Coordinator) toNextStage() {
	switch c.CoordinatorStage {
	case MapStage:
		// log.Printf("[Coordinator] All map tasks finished.\n")
		c.CoordinatorStage = ReduceStage
		c.registerReduceTasks()
	case ReduceStage:
		// log.Printf("[Coordinator] All reduce tasks finished.\n")
		c.CoordinatorStage = FinishedStage
	case FinishedStage:
		// Do nothing
	}
}

func (c *Coordinator) FetchTask(args *FetchTaskArgs, reply *FetchTaskReply) error {
	// log.Printf("[Coordinator] Worker node %s asks for a task. Msg: %s\n", args.NodeId, args.Msg)
	reply.NodeId = c.Id
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	select {
	case task := <-c.TaskChannel:
		c.TaskSet.StartTask(task)
		reply.Task = task
		reply.Msg = "Task fetched successfully."
	default:
		// log.Println("[Coordinator] No task is available right now.")
		if c.checkStageComplete() {
			c.toNextStage()
			// log.Printf("[Coordinator] Move to next stage: %d\n", c.CoordinatorStage)
		}
		if c.CoordinatorStage == FinishedStage {
			reply.Task = &Task{Type: ExitTask}
			reply.Msg = "All tasks done. Exit gracefully."
		} else {
			reply.Task = &Task{Type: WaitTask}
			reply.Msg = "No available task to distribute. Wait for a while and retry."
		}
	}
	return nil
}

func (c *Coordinator) registerReduceTasks() {
	for i := 0; i < c.ReducerTaskNum; i++ {
		task := &Task{
			Type:   ReduceTask,
			TaskId: i,
			Files:  []string{},
		}
		for j := 0; j < c.MapperTaskNum; j++ {
			file := fmt.Sprintf(IntermediateNameTemplate, j, i)
			task.Files = append(task.Files, file)
		}
		c.TaskSet.RegisterTask(task)
		go func() { c.TaskChannel <- task }()
	}
	// log.Println("[Coordinator] Successfully registered reduce tasks.")
}

func (c *Coordinator) SubmitTask(args *SubmitTaskArgs, reply *SubmitTaskReply) error {
	// log.Printf("[Coordinator] Worker node %s submits finished task. TaskType: %d, TaskId: %d, Msg: %s\n",args.NodeId, args.Task.Type, args.Task.TaskId, args.Msg,)
	reply.NodeId = c.Id
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	if c.TaskSet.CompleteTask(args.Task) {
		reply.Msg = "Submitted task successfully."
	} else {
		reply.Msg = "Task already completed by another worker, no worry."
	}
	return nil
}

// timeoutDetecter continuously check if there are timeout tasks,
// if found, reassign the task, insert it to the queue.
func (c *Coordinator) timeoutDetecter() {
	for {
		c.Mutex.Lock()
		var timedOutTasks []*Task
		switch c.CoordinatorStage {
		case MapStage:
			timedOutTasks = c.TaskSet.CheckTimeout(MapTask)
		case ReduceStage:
			timedOutTasks = c.TaskSet.CheckTimeout(ReduceTask)
		case FinishedStage:
			c.Mutex.Unlock()
			return
		}
		for _, task := range timedOutTasks {
			// log.Printf("[Coordinator] Task timed out, register again. TaskType: %d, TaskId: %d\n",task.Type, task.TaskId)
			c.TaskSet.RegisterTask(task)
			go func(task *Task) { c.TaskChannel <- task }(task)
		}
		c.Mutex.Unlock()
		time.Sleep(TimeoutCheckInterval * time.Second)
	}
}

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		// log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	c.Mutex.Lock()
	if c.CoordinatorStage == FinishedStage {
		c.Mutex.Unlock()
		// log.Printf("[Coordinator] All task done. Exit gracefully.\n")
		// Wait for a while to make sure all workers have exited.
		// Double the time interval for all workers can receive ExitTask before coordinator exits.
		time.Sleep(2 * WaitInterval * time.Second)
		// log.Printf("[Coordinator] Exiting...\n")
		return true
	} else {
		c.Mutex.Unlock()
		return false
	}
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{
		Id:               uuid.New(),
		Files:            files,
		MapperTaskNum:    len(files),
		ReducerTaskNum:   nReduce,
		TaskChannel:      make(chan *Task, ChannelSize),
		CoordinatorStage: MapStage,
		TaskSet:          NewTaskSet(),
	}
	// log.Printf("[Coordinator] Coordinator %s is running!\n", c.Id)

	c.registerMapTasks()

	go c.timeoutDetecter()

	c.server()
	return &c
}
