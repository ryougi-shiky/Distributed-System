package mr

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
)

type Coordinator struct {
	// Your definitions here.
	State int // 0 is idle; 1 is mapping; 2 is reducing; 3 is finished
	// NumMapWorkers    int
	// NumReduceWorkers int
	NumMapTasks    int
	NumReduceTasks int
	MapTasks       chan Task
	MapTasksFin    chan bool
	ReduceTasks    chan Task
	ReduceTasksFin chan bool
}

type Task struct {
	FileName string
	// State    int // 0 is idle; 1 is in progress; 2 is done
	ID int
}

// Your code here -- RPC handlers for the worker to call.

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

func (c *Coordinator) GetTask(args *TaskRequest, reply *TaskResponse) error {
	if len(c.MapTasksFin) != c.NumMapTasks {
		// retrieve a map task from channel
		maptask, isReceived := <-c.MapTasks
		if isReceived {
			reply.RespTask = maptask
			fmt.Printf("Map task %s is assigned\n", maptask.FileName)
		}
	} else {
		reduceTask, isReceived := <-c.ReduceTasks
		if isReceived {
			reply.RespTask = reduceTask
			fmt.Printf("Reduce task %d is assigned\n", reduceTask.ID)
		} else {
			fmt.Println("All tasks are done")
		}
	}
	reply.NumMapTasks = c.NumMapTasks
	reply.NumReduceTasks = c.NumReduceTasks
	reply.State = c.State
	return nil
}

func (c *Coordinator) FinishTask(args *TaskRequest, reply *TaskResponse) error {
	if len(c.MapTasksFin) != c.NumMapTasks {
		c.MapTasksFin <- true
		if len(c.MapTasksFin) == c.NumMapTasks {
			c.State = 2
		}
	} else if len(c.ReduceTasksFin) != c.NumReduceTasks {
		c.ReduceTasksFin <- true
		if len(c.ReduceTasksFin) == c.NumReduceTasks {
			c.State = 3
		}
	}
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
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	ret := false
	if len(c.ReduceTasksFin) == c.NumReduceTasks {
		ret = true
	}
	// Your code here.

	return ret
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{
		State: 0,
		// NumMapWorkers:    3,
		// NumReduceWorkers: nReduce,
		NumMapTasks:    len(files),
		NumReduceTasks: nReduce,
		MapTasks:       make(chan Task, len(files)),
		ReduceTasks:    make(chan Task, nReduce),
		MapTasksFin:    make(chan bool, len(files)),
		ReduceTasksFin: make(chan bool, nReduce),
	}
	// Your code here.
	for i, file := range files {
		c.MapTasks <- Task{FileName: file, ID: i}

	}

	for i := 0; i < nReduce; i++ {
		c.ReduceTasks <- Task{ID: i}
	}

	c.server()
	return &c
}
