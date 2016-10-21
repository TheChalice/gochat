package main

import (
	"fmt"
	"log"
	"net"
	"bufio"
)

func main() {
	localAddr := ":5678"
	
	chatServer := ChatServer {
		NewConns:        make(chan net.Conn, 8),
		ClosedConns:     make(chan net.Conn, 8),
		Visitors:        make(map[net.Conn]*Visitor, 32),
		PendingMessages: make(chan string, 16),
	}
	
	go chatServer.Run() // start new goroutine/thread
	
	var listener, err = net.Listen("tcp", localAddr)
	if err != nil {
		log.Fatalf("Listen error: %s", err)
	}
	
	log.Printf("Listening at %s", localAddr)

	for {
		var conn, err = listener.Accept()
		if err != nil {
			log.Printf("Accept error: %s", err)
		} else {
			chatServer.NewConns <- conn // write data into channel
		}
	}
}

type ChatServer struct {
	NewConns        chan net.Conn         // new connection channel
	ClosedConns     chan net.Conn         // closed connection channel
	Visitors        map[net.Conn]*Visitor // hashtable, visitors
	NextVisitorId   int
	PendingMessages chan string           // message channel
}

func (server *ChatServer) Run() {
	for {
		select {
		case conn := <- server.NewConns:
		
			visitor := &Visitor{
				Server: server, 
				Conn:   conn, 
				Name:   fmt.Sprintf("visitor#%d", server.NextVisitorId),
				
				ClientReader:    bufio.NewReader(conn),
				PendingMessages: make(chan string, 10),
				CloseSignal:     make(chan int, 3),
				Closed:          make(chan struct{}),
			}
			server.Visitors[conn] = visitor
			server.NextVisitorId++
			
			go visitor.Run()
			
		case conn := <- server.ClosedConns:
		
			delete(server.Visitors, conn)
			
		case msg := <- server.PendingMessages:
			
			// broadcast
			for _, visitor := range server.Visitors {
				select {
				default:
					visitor.CloseSignal <- 2
				case visitor.PendingMessages <- msg:
				}
			}
		}
	}
}

type Visitor struct {
	Server *ChatServer
	Conn   net.Conn
	Name   string
	
	ClientReader    *bufio.Reader // read client
	PendingMessages chan string   // message channel
	CloseSignal     chan int      // a channel to notify closing
	Closed          chan struct{} // a channel to check if closed
}

func (visitor *Visitor) Run() {
	
	// read from client
	go func() {
		for {
			select {
			case <-visitor.Closed:
				return
			default:
				msg, err := visitor.ClientReader.ReadString('\n')
				if err != nil {
					log.Printf("visitor read error: %s", err)
					visitor.CloseSignal <- 0
					return
				}
				visitor.Server.PendingMessages <- msg
			}
		}
	}()
	
	// write to client
	go func() {
		for {
			select {
			case <-visitor.Closed:
				return
			case msg := <- visitor.PendingMessages:
				_, err := visitor.Conn.Write([]byte(msg))
				if err != nil {
					log.Printf("visitor write error: %s", err)
					visitor.CloseSignal <- 1
					return
				}
			}
		}
	}()
	
	// close
	defer func() {
		log.Printf("%s exited", visitor.Name)
		
		visitor.Conn.Close()
		visitor.Server.ClosedConns <- visitor.Conn
	}()

	// monitor close signal
	<- visitor.CloseSignal
	close(visitor.Closed)
}