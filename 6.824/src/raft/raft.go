package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	//	"bytes"
	"bytes"
	"log"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	//	"6.824/labgob"
	"6.824/labgob"
	"6.824/labrpc"
)

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type AppendEntriesArgs struct {
	Term         int        // leader’s term
	LeaderId     int        // with leaderId follower can redirect clients
	PrevLogIndex int        // index of log entry immediately preceding new ones
	PrevLogTerm  int        // term of prevLogIndex entry
	Entries      []LogEntry // log entries to store (empty for heartbeat; may send more than one for efficiency)
	LeaderCommit int        // leader's commitIndex
}

type AppendEntriesReply struct {
	Term    int  // currentTerm, for leader to update itself
	XTerm   int  // term in the conflicting entry (if any)
	XIndex  int  // index of first entry with that term (if any)
	XLen    int  // log length
	Success bool // true if follower contained entry matching prevLogIndex and prevLogTerm
}

type RaftState int
type LogEntries []LogEntry

const (
	FollowerState RaftState = iota
	CandidateState
	LeaderState
)

// A Go object implementing raft log entry
type LogEntry struct {
	Command interface{} // Command to be excuted
	Term    int         // Term number when created
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	leaderId int // the leader server that current server recognizes

	state         RaftState
	heartbeatTime time.Time
	electionTime  time.Time

	// Persistent state on all servers

	currentTerm int        // latest term server has seen
	votedFor    int        // candidateId that received vote in current term
	log         LogEntries // log entries; each entry contains command for state machine, and term when entry was received by leader

	// Volatile state on all servers

	commitIndex int // index of highest log entry known to be committed
	lastApplied int // index of highest log entry applied to state machine

	// Volatile state on leaders (Reinitialized after election)

	nextIndex  []int // for each server, index of the next log entry to send to that server
	matchIndex []int // for each server, index of highest log entry known to be replicated on server
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	// var term int
	// var isleader bool
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == LeaderState
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
func (rf *Raft) persist() {
	Debug(dPersist, "S%d Saving persistent state to stable storage at T%d.", rf.me, rf.currentTerm)
	// Your code here (2C).
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	if err := e.Encode(rf.currentTerm); err != nil {
		Debug(dError, "Raft.readPersist: failed to decode \"rf.currentTerm\". err: %v, data: %v", err, rf.currentTerm)
	}
	if err := e.Encode(rf.votedFor); err != nil {
		Debug(dError, "Raft.readPersist: failed to decode \"rf.votedFor\". err: %v, data: %v", err, rf.votedFor)
	}
	if err := e.Encode(rf.log); err != nil {
		Debug(dError, "Raft.readPersist: failed to decode \"rf.log\". err: %v, data: %v", err, rf.log)
	}
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	Debug(dPersist, "S%d Restoring previously persisted state at T%d.", rf.me, rf.currentTerm)
	// Your code here (2C).
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if err := d.Decode(&rf.currentTerm); err != nil {
		Debug(dError, "Raft.readPersist: failed to decode \"rf.currentTerm\". err: %v, data: %s", err, data)
	}
	if err := d.Decode(&rf.votedFor); err != nil {
		Debug(dError, "Raft.readPersist: failed to decode \"rf.votedFor\". err: %v, data: %s", err, data)
	}
	if err := d.Decode(&rf.log); err != nil {
		Debug(dError, "Raft.readPersist: failed to decode \"rf.log\". err: %v, data: %s", err, data)
	}
}

// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int // candidate's term
	CandidateId  int // candidate requesting vote
	LastLogIndex int // index of candidate’s last log entry (§5.4)
	LastLogTerm  int // term of candidate’s last log entry (§5.4)
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int  // currentTerm, for candidate to update itself
	VoteGranted bool // true means candidate received vote
}

// example RequestVote RPC handler.
// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	Debug(dVote, "S%d <- S%d Received vote request at T%d.", rf.me, args.CandidateId, rf.currentTerm)
	reply.VoteGranted = false

	// Reply false if term < currentTerm (§5.1)
	if args.Term < rf.currentTerm {
		Debug(dVote, "S%d Term is lower, rejecting the vote. (%d < %d)", rf.me, args.Term, rf.currentTerm)
		reply.Term = rf.currentTerm
		return
	}

	rf.checkTerm(args.Term)
	reply.Term = rf.currentTerm

	// If votedFor is null or candidateId,
	// and candidate’s log is at least as up-to-date as receiver’s log, grant vote (§5.2, §5.4)
	lastLogIndex, lastLogTerm := rf.log.lastLogInfo()
	if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
		if lastLogTerm < args.LastLogTerm ||
			(lastLogTerm == args.LastLogTerm && lastLogIndex <= args.LastLogIndex) {
			Debug(dVote, "S%d Granting vote to S%d at T%d.", rf.me, args.CandidateId, args.Term)
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.persist()
			Debug(dTimer, "S%d Resetting ELT, wait for next potential election timeout.", rf.me)
			rf.setElectionTimeout(randElectionTimeout())
		} else {
			Debug(dVote, "S%d Candidate's log not up-to-date, rejecting the vote. LLI: %d, %d. LLT: %d, %d.",
				rf.me, lastLogIndex, args.LastLogIndex, lastLogTerm, args.LastLogTerm)
		}
	} else {
		Debug(dVote, "S%d Already voted for S%d, rejecting the vote.", rf.me, rf.votedFor)
	}
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state != LeaderState {
		return -1, -1, false
	}
	logEntry := LogEntry{
		Command: command,
		Term:    rf.currentTerm,
	}
	rf.log = append(rf.log, logEntry)
	index = len(rf.log)
	term = rf.currentTerm
	Debug(dLog, "S%d Add command at T%d. LI: %d, Command: %v\n", rf.me, term, index, command)
	rf.persist()
	rf.sendEntries(false)

	return index, term, isLeader
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// These constants are all in milliseconds.
const TickInterval int64 = 30 // Loop interval for ticker and applyLogsLoop. Mostly for checking timeouts.

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) ticker() {
	for rf.killed() == false {

		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().
		rf.mu.Lock()
		// leader repeat heartbeat during idle periods to prevent election timeouts (§5.2)
		if rf.state == LeaderState && time.Now().After(rf.heartbeatTime) {
			Debug(dTimer, "S%d HBT elapsed. Broadcast heartbeats.", rf.me)
			rf.sendEntries(true)
		}
		// If election timeout elapses: start new election
		if time.Now().After(rf.electionTime) {
			Debug(dTimer, "S%d ELT elapsed. Converting to Candidate, calling election.", rf.me)
			rf.raiseElection()
		}
		rf.mu.Unlock()
		time.Sleep(time.Duration(TickInterval) * time.Millisecond)
	}
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {

	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.leaderId = -1
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.state = FollowerState
	rf.setElectionTimeout(randHeartbeatTimeout())
	rf.log = make([]LogEntry, 0)
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	for peer := range rf.peers {
		rf.nextIndex[peer] = len(rf.log) + 1
	}

	lastLogIndex, lastLogTerm := rf.log.lastLogInfo()
	Debug(dClient, "S%d Started at T%d. LLI: %d, LLT: %d.", rf.me, rf.currentTerm, lastLogIndex, lastLogTerm)

	// start ticker goroutine to start elections
	go rf.ticker()

	// Apply logs periodically until the last committed index to make sure state machine is up to date.
	go rf.applyLogsLoop(applyCh)

	return rf
}

// index and term start from 1
func (logEntries LogEntries) getEntry(index int) *LogEntry {
	if index < 0 {
		log.Panic("LogEntries.getEntry: index < 0.\n")
	}
	if index == 0 {
		return &LogEntry{
			Command: nil,
			Term:    0,
		}
	}
	if index > len(logEntries) {
		return &LogEntry{
			Command: nil,
			Term:    -1,
		}
	}
	return &logEntries[index-1]
}

func (logEntries LogEntries) lastLogInfo() (index, term int) {
	index = len(logEntries)
	logEntry := logEntries.getEntry(index)
	return index, logEntry.Term
}

func (logEntries LogEntries) getSlice(startIndex, endIndex int) LogEntries {
	if startIndex <= 0 {
		Debug(dError, "LogEntries.getSlice: startIndex out of range. startIndex: %d, len: %d.",
			startIndex, len(logEntries))
		log.Panic("LogEntries.getSlice: startIndex out of range. \n")
	}
	if endIndex > len(logEntries)+1 {
		Debug(dError, "LogEntries.getSlice: endIndex out of range. endIndex: %d, len: %d.",
			endIndex, len(logEntries))
		log.Panic("LogEntries.getSlice: endIndex out of range.\n")
	}
	if startIndex > endIndex {
		Debug(dError, "LogEntries.getSlice: startIndex > endIndex. (%d > %d)", startIndex, endIndex)
		log.Panic("LogEntries.getSlice: startIndex > endIndex.\n")
	}
	return logEntries[startIndex-1 : endIndex-1]
}

// These constants are all in milliseconds.
const BaseHeartbeatTimeout int64 = 300 // Lower bound of heartbeat timeout. Election is raised when timeout as a follower.
const BaseElectionTimeout int64 = 1000 // Lower bound of election timeout. Another election is raised when timeout as a candidate.

const RandomFactor float64 = 0.8 // Factor to control upper bound of heartbeat timeouts and election timeouts.

func randElectionTimeout() time.Duration {
	extraTime := int64(float64(rand.Int63()%BaseElectionTimeout) * RandomFactor)
	return time.Duration(extraTime+BaseElectionTimeout) * time.Millisecond
}

func randHeartbeatTimeout() time.Duration {
	extraTime := int64(float64(rand.Int63()%BaseHeartbeatTimeout) * RandomFactor)
	return time.Duration(extraTime+BaseHeartbeatTimeout) * time.Millisecond
}

func (rf *Raft) setElectionTimeout(timeout time.Duration) {
	t := time.Now()
	t = t.Add(timeout)
	rf.electionTime = t
}

// These constants are all in milliseconds.
const HeartbeatInterval int64 = 100 // Interval between heartbeats. Broadcast heartbeats when timeout as a leader.

func (rf *Raft) setHeartbeatTimeout(timeout time.Duration) {
	t := time.Now()
	t = t.Add(timeout)
	rf.heartbeatTime = t
}

// If RPC request or response contains term T > currentTerm: set currentTerm = T, convert to follower (§5.1)
// Check if current term is out of date when hearing from other peers,
// update term, revert to follower state and return true if necesarry
func (rf *Raft) checkTerm(term int) bool {
	if rf.currentTerm < term {
		Debug(dTerm, "S%d Term is higher, updating term to T%d, setting state to follower. (%d > %d)",
			rf.me, term, term, rf.currentTerm)
		rf.state = FollowerState
		rf.currentTerm = term
		rf.votedFor = -1
		rf.persist()
		return true
	}
	return false
}

func (rf *Raft) raiseElection() {
	rf.state = CandidateState
	// Increment currentTerm
	rf.currentTerm++
	Debug(dTerm, "S%d Starting a new term. Now at T%d.", rf.me, rf.currentTerm)
	// Vote for self
	rf.votedFor = rf.me
	rf.persist()
	// Reset election timer
	rf.setElectionTimeout(randElectionTimeout())
	Debug(dTimer, "S%d Resetting ELT because of election, wait for next potential election timeout.", rf.me)
	lastLogIndex, lastLogTerm := rf.log.lastLogInfo()
	args := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	voteCount := 1
	var once sync.Once
	// Send RequestVote RPCs to all other servers
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		Debug(dVote, "S%d -> S%d Sending request vote at T%d.", rf.me, peer, rf.currentTerm)
		go rf.candidateRequestVote(&voteCount, args, &once, peer)
	}
}

func (rf *Raft) candidateRequestVote(voteCount *int, args *RequestVoteArgs, once *sync.Once, server int) {
	reply := &RequestVoteReply{}
	ok := rf.sendRequestVote(server, args, reply)
	if ok {
		rf.mu.Lock()
		defer rf.mu.Unlock()
		Debug(dVote, "S%d <- S%d Received request vote reply at T%d.", rf.me, server, rf.currentTerm)
		if reply.Term < rf.currentTerm {
			Debug(dVote, "S%d Term is lower, invalid vote reply. (%d < %d)", rf.me, reply.Term, rf.currentTerm)
			return
		}
		if rf.currentTerm != args.Term {
			Debug(dWarn, "S%d Term has changed after the vote request, vote reply discarded. "+
				"requestTerm: %d, currentTerm: %d.", rf.me, args.Term, rf.currentTerm)
			return
		}
		// If AppendEntries RPC received from new leader: convert to follower
		rf.checkTerm(reply.Term)
		if reply.VoteGranted {
			*voteCount++
			Debug(dVote, "S%d <- S%d Get a yes vote at T%d.", rf.me, server, rf.currentTerm)
			// If votes received from majority of servers: become leader
			if *voteCount > len(rf.peers)/2 {
				once.Do(func() {
					Debug(dLeader, "S%d Received majority votes at T%d. Become leader.", rf.me, rf.currentTerm)
					rf.state = LeaderState
					lastLogIndex, _ := rf.log.lastLogInfo()
					for peer := range rf.peers {
						rf.nextIndex[peer] = lastLogIndex + 1
						rf.matchIndex[peer] = 0
					}
					// Upon election: send initial empty AppendEntries RPCs (heartbeat) to each server
					rf.sendEntries(true)
				})
			}
		} else {
			Debug(dVote, "S%d <- S%d Get a no vote at T%d.", rf.me, server, rf.currentTerm)
		}
	}
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if len(args.Entries) == 0 {
		Debug(dLog2, "S%d <- S%d Received heartbeat at T%d.", rf.me, args.LeaderId, rf.currentTerm)
	} else {
		Debug(dLog2, "S%d <- S%d Received append entries at T%d.", rf.me, args.LeaderId, rf.currentTerm)
	}
	reply.Success = false

	// Reply false if term < currentTerm (§5.1)
	if args.Term < rf.currentTerm {
		Debug(dLog2, "S%d Term is lower, rejecting append request. (%d < %d)",
			rf.me, args.Term, rf.currentTerm)
		reply.Term = rf.currentTerm
		return
	}

	// For Candidates. If AppendEntries RPC received from new leader: convert to follower
	if rf.state == CandidateState && rf.currentTerm == args.Term {
		rf.state = FollowerState
		Debug(dLog2, "S%d Convert from candidate to follower at T%d.", rf.me, rf.currentTerm)
	}

	rf.checkTerm(args.Term)
	reply.Term = rf.currentTerm

	Debug(dTimer, "S%d Resetting ELT, wait for next potential heartbeat timeout.", rf.me)
	rf.setElectionTimeout(randHeartbeatTimeout())

	rf.leaderId = args.LeaderId

	// Reply false if log doesn’t contain an entry at prevLogIndex whose term matches prevLogTerm (§5.3)
	if args.PrevLogTerm == -1 || args.PrevLogTerm != rf.log.getEntry(args.PrevLogIndex).Term {
		Debug(dLog2, "S%d Prev log entries do not match. Ask leader to retry.", rf.me)
		reply.XLen = len(rf.log)
		reply.XTerm = rf.log.getEntry(args.PrevLogIndex).Term
		reply.XIndex, _ = rf.log.getBoundsWithTerm(reply.XTerm)
		return
	}
	// If an existing entry conflicts with a new one (same index but different terms),
	// delete the existing entry and all that follow it (§5.3)
	for i, entry := range args.Entries {
		if rf.log.getEntry(i+1+args.PrevLogIndex).Term != entry.Term {
			Debug(dLog2, "S%d Running into conflict with existing entries at T%d. conflictLogs: %v, startIndex: %d.",
				rf.me, rf.currentTerm, args.Entries[i:], i+1+args.PrevLogIndex)
			rf.log = append(rf.log.getSlice(1, i+1+args.PrevLogIndex), args.Entries[i:]...)
			break
		}
	}
	Debug(dLog2, "S%d <- S%d Append entries success. Saved logs: %v.", rf.me, args.LeaderId, args.Entries)
	if len(args.Entries) > 0 {
		rf.persist()
	}
	if args.LeaderCommit > rf.commitIndex {
		Debug(dCommit, "S%d Get higher LC at T%d, updating commitIndex. (%d < %d)",
			rf.me, rf.currentTerm, rf.commitIndex, args.LeaderCommit)
		rf.commitIndex = min(args.LeaderCommit, args.PrevLogIndex+len(args.Entries))
		Debug(dCommit, "S%d Updated commitIndex at T%d. CI: %d.", rf.me, rf.currentTerm, rf.commitIndex)
	}
	reply.Success = true
}

func (rf *Raft) sendEntries(isHeartbeat bool) {
	Debug(dTimer, "S%d Resetting HBT, wait for next heartbeat broadcast.", rf.me)
	rf.setHeartbeatTimeout(time.Duration(HeartbeatInterval) * time.Millisecond)
	Debug(dTimer, "S%d Resetting ELT, wait for next potential heartbeat timeout.", rf.me)
	rf.setElectionTimeout(randHeartbeatTimeout())
	lastLogIndex, _ := rf.log.lastLogInfo()
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		nextIndex := rf.nextIndex[peer]
		args := &AppendEntriesArgs{
			Term:         rf.currentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: nextIndex - 1,
			PrevLogTerm:  rf.log.getEntry(nextIndex - 1).Term,
			LeaderCommit: rf.commitIndex,
		}
		if lastLogIndex >= nextIndex {
			// If last log index ≥ nextIndex for a follower:
			// send AppendEntries RPC with log entries starting at nextIndex
			entries := make([]LogEntry, lastLogIndex-nextIndex+1)
			copy(entries, rf.log.getSlice(nextIndex, lastLogIndex+1))
			args.Entries = entries
			Debug(dLog, "S%d -> S%d Sending append entries at T%d. PLI: %d, PLT: %d, LC: %d. Entries: %v.",
				rf.me, peer, rf.currentTerm, args.PrevLogIndex,
				args.PrevLogTerm, args.LeaderCommit, args.Entries,
			)
			go rf.leaderSendEntries(args, peer)
		} else if isHeartbeat {
			args.Entries = make([]LogEntry, 0)
			Debug(dLog, "S%d -> S%d Sending heartbeat at T%d. PLI: %d, PLT: %d, LC: %d.",
				rf.me, peer, rf.currentTerm, args.PrevLogIndex, args.PrevLogTerm, args.LeaderCommit)
			go rf.leaderSendEntries(args, peer)
		}
	}
}

func (rf *Raft) leaderSendEntries(args *AppendEntriesArgs, server int) {
	reply := &AppendEntriesReply{}
	ok := rf.sendAppendEntries(server, args, reply)
	if ok {
		rf.mu.Lock()
		defer rf.mu.Unlock()
		Debug(dLog, "S%d <- S%d Received send entry reply at T%d.", rf.me, server, rf.currentTerm)
		if reply.Term < rf.currentTerm {
			Debug(dLog, "S%d Term lower, invalid send entry reply. (%d < %d)",
				rf.me, reply.Term, rf.currentTerm)
			return
		}
		if rf.currentTerm != args.Term {
			Debug(dWarn, "S%d Term has changed after the append request, send entry reply discarded."+
				"requestTerm: %d, currentTerm: %d.", rf.me, args.Term, rf.currentTerm)
			return
		}
		if rf.checkTerm(reply.Term) {
			return
		}
		// If successful: update nextIndex and matchIndex for follower (§5.3)
		if reply.Success {
			Debug(dLog, "S%d <- S%d Log entries in sync at T%d.", rf.me, server, rf.currentTerm)
			newNext := args.PrevLogIndex + 1 + len(args.Entries)
			newMatch := args.PrevLogIndex + len(args.Entries)
			rf.nextIndex[server] = max(newNext, rf.nextIndex[server])
			rf.matchIndex[server] = max(newMatch, rf.matchIndex[server])
			// If there exists an N such that N > commitIndex, a majority of matchIndex[i] ≥ N,
			// and log[N].term == currentTerm: set commitIndex = N (§5.3, §5.4).
			for N := len(rf.log); N > rf.commitIndex && rf.log.getEntry(N).Term == rf.currentTerm; N-- {
				count := 1
				for peer, matchIndex := range rf.matchIndex {
					if peer == rf.me {
						continue
					}
					if matchIndex >= N {
						count++
					}
				}
				if count > len(rf.peers)/2 {
					rf.commitIndex = N
					Debug(dCommit, "S%d Updated commitIndex at T%d for majority consensus. CI: %d.", rf.me, rf.currentTerm, rf.commitIndex)
					break
				}
			}
		} else {
			// If AppendEntries fails because of log inconsistency: decrement nextIndex and retry (§5.3)
			// the optimization that backs up nextIndex by more than one entry at a time
			if reply.XTerm == -1 {
				// follower's log is too short
				rf.nextIndex[server] = reply.XLen + 1
			} else {
				_, maxIndex := rf.log.getBoundsWithTerm(reply.XTerm)
				if maxIndex != -1 {
					// leader has XTerm
					rf.nextIndex[server] = maxIndex
				} else {
					// leader doesn't have XTerm
					rf.nextIndex[server] = reply.XIndex
				}
			}
			lastLogIndex, _ := rf.log.lastLogInfo()
			nextIndex := rf.nextIndex[server]
			if lastLogIndex >= nextIndex {
				Debug(dLog, "S%d <- S%d Inconsistent logs, retrying.", rf.me, server)
				entries := make([]LogEntry, lastLogIndex-nextIndex+1)
				copy(entries, rf.log.getSlice(nextIndex, lastLogIndex+1))
				newArg := &AppendEntriesArgs{
					Term:         rf.currentTerm,
					LeaderId:     rf.me,
					PrevLogIndex: nextIndex - 1,
					PrevLogTerm:  rf.log.getEntry(nextIndex - 1).Term,
					Entries:      entries,
				}
				go rf.leaderSendEntries(newArg, server)
			}
		}
	}
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	if ok {
		rf.mu.Lock()
		defer rf.mu.Unlock()

		// 检查响应中的任期号，可能需要更新当前节点的任期
		if reply.Term > rf.currentTerm {
			rf.currentTerm = reply.Term
			rf.state = FollowerState
			rf.votedFor = -1
			rf.persist()
		}

		if rf.state == LeaderState && args.Term == rf.currentTerm {
			if reply.Success {
				// 成功复制了日志条目
				rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)
				rf.nextIndex[server] = rf.matchIndex[server] + 1
			} else {
				// 减少 nextIndex 重试
				rf.nextIndex[server] = args.PrevLogIndex
			}
		}
	}
	return ok
}

func (rf *Raft) applyLogsLoop(applyCh chan ApplyMsg) {
	for !rf.killed() {
		// Apply logs periodically until the last committed index.
		rf.mu.Lock()
		// To avoid the apply operation getting blocked with the lock held,
		// use a slice to store all committed msgs to apply, and apply them only after unlocked
		appliedMsgs := []ApplyMsg{}
		for rf.commitIndex > rf.lastApplied {
			rf.lastApplied++
			appliedMsgs = append(appliedMsgs, ApplyMsg{
				CommandValid: true,
				Command:      rf.log.getEntry(rf.lastApplied).Command,
				CommandIndex: rf.lastApplied,
			})
			Debug(dLog2, "S%d Applying log at T%d. LA: %d, CI: %d.", rf.me, rf.currentTerm, rf.lastApplied, rf.commitIndex)
		}
		rf.mu.Unlock()
		for _, msg := range appliedMsgs {
			applyCh <- msg
		}
		time.Sleep(time.Duration(TickInterval) * time.Millisecond)
	}
}

// Get the index of first entry and last entry with the given term.
// Return (-1,-1) if no such term is found
func (logEntries LogEntries) getBoundsWithTerm(term int) (minIndex int, maxIndex int) {
	if term == 0 {
		return 0, 0
	}
	minIndex, maxIndex = math.MaxInt, -1
	for i := 1; i <= len(logEntries); i++ {
		if logEntries.getEntry(i).Term == term {
			minIndex = min(minIndex, i)
			maxIndex = max(maxIndex, i)
		}
	}
	if maxIndex == -1 {
		return -1, -1
	}
	return
}
