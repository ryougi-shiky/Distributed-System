package mr

import (
	"time"
)

type TaskType int

const (
	MapTask TaskType = iota
	ReduceTask
	ExitTask
	WaitTask
)

type Task struct {
	Type   TaskType
	TaskId int
	// For map task, ReducerTaskNum is the number of reduce tasks.
	ReducerTaskNum int
	Files          []string
}

func NewTask(taskType TaskType, taskId int, nReduce int, files []string) *Task {
	return &Task{
		Type:           taskType,
		TaskId:         taskId,
		ReducerTaskNum: nReduce,
		Files:          files,
	}
}

type TaskStatus int

const (
	Waiting TaskStatus = iota
	Working
	Done
)

type TaskMetaData struct {
	Status    TaskStatus
	StartTime time.Time
	Task      *Task
}

func NewTaskMetaData(task *Task) *TaskMetaData {
	return &TaskMetaData{
		Status: Waiting,
		Task:   task,
	}
}

type TaskSet struct {
	mapTaskMap    map[int]*TaskMetaData
	reduceTaskMap map[int]*TaskMetaData
}

func NewTaskSet() *TaskSet {
	return &TaskSet{
		mapTaskMap:    map[int]*TaskMetaData{},
		reduceTaskMap: map[int]*TaskMetaData{},
	}
}

func (ts *TaskSet) RegisterTask(task *Task) {
	taskMetaData := NewTaskMetaData(task)
	switch task.Type {
	case MapTask:
		ts.mapTaskMap[task.TaskId] = taskMetaData
	case ReduceTask:
		ts.reduceTaskMap[task.TaskId] = taskMetaData
	default:
		// log.Panic("Cannot add unsupported task to TaskSet.")
	}
}

// Update the task state as working, and set the start time.
func (ts *TaskSet) StartTask(task *Task) {
	var taskMetaData *TaskMetaData
	switch task.Type {
	case MapTask:
		taskMetaData = ts.mapTaskMap[task.TaskId]
	case ReduceTask:
		taskMetaData = ts.reduceTaskMap[task.TaskId]
	default:
		// log.Panic("Cannot get unsupported task from TaskSet.")
	}
	taskMetaData.StartTime = time.Now()
	taskMetaData.Status = Working
}

// CompleteTask marks a task as completed.
func (ts *TaskSet) CompleteTask(task *Task) bool {
	var taskMetaData *TaskMetaData
	switch task.Type {
	case MapTask:
		taskMetaData = ts.mapTaskMap[task.TaskId]
	case ReduceTask:
		taskMetaData = ts.reduceTaskMap[task.TaskId]
	default:
		// log.Panic("Cannot get unsupported task from TaskSet.")
	}
	if taskMetaData.Status != Done {
		// log.Printf("Task already completed, thus result abandoned. Task: %v\n", taskMetaData)
		return false
	}
	taskMetaData.Status = Done
	return true
}

// CheckComplete checks if all tasks of a certain type are completed.
func (ts *TaskSet) CheckComplete(taskType TaskType) bool {
	var m map[int]*TaskMetaData
	switch taskType {
	case MapTask:
		m = ts.mapTaskMap
	case ReduceTask:
		m = ts.reduceTaskMap
	default:
		// log.Panic("Cannot check unsupported task type in TaskSet.")
	}
	for _, taskInfo := range m {
		if taskInfo.Status != Done {
			return false
		}
	}
	return true
}

// TaskTimeout is the time limit for a task to be completed. Set to 10 seconds.
const TaskTimeout time.Duration = 10

// CheckTimeout checks if any task of a certain type has timed out. Returns a list of timed out tasks.
func (ts *TaskSet) CheckTimeout(taskType TaskType) []*Task {
	var res []*Task
	var m map[int]*TaskMetaData
	switch taskType {
	case MapTask:
		m = ts.mapTaskMap
	case ReduceTask:
		m = ts.reduceTaskMap
	default:
		// log.Panic("Cannot check unsupported task type in TaskSet.")
	}
	for _, taskInfo := range m {
		if taskInfo.Status == Working &&
			time.Since(taskInfo.StartTime) > TaskTimeout*time.Second {
			res = append(res, taskInfo.Task)
		}
	}
	return res
}
