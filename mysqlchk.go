package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"net/http"

	"github.com/go-sql-driver/mysql"
)

var db *sql.DB
var wsrepStmt *sql.Stmt
var readOnlyStmt *sql.Stmt

var defaultTimeout = time.Duration(10) * time.Second

var username = flag.String("username", "clustercheckuser", "MySQL Username")
var password = flag.String("password", "clustercheckpassword!", "MySQL Password")
var socket = flag.String("socket", "", "MySQL UNIX Socket")
var host = flag.String("host", "localhost", "MySQL Server")
var port = flag.Int("port", 3306, "MySQL Port")
var timeout = flag.Duration("timeout", defaultTimeout, "MySQL connection timeout")
var availableWhenDonor = flag.Bool("donor", false, "Cluster available while node is a donor")
var availableWhenReadonly = flag.Bool("readonly", false, "Cluster available while node is read only")
var forceFailFile = flag.String("failfile", "/dev/shm/proxyoff", "Create this file to manually fail checks")
var forceUpFile = flag.String("upfile", "/dev/shm/proxyon", "Create this file to manually pass checks")
var bindPort = flag.Int("bindport", 9200, "MySQLChk bind port")
var bindAddr = flag.String("bindaddr", "", "MySQLChk bind address")
var allowCleartextPasswords = flag.Bool("cleartext", true, "Allow cleartext passwords")

func init() {
	flag.Parse()
}

func checkHandler(w http.ResponseWriter, r *http.Request) {
	var fieldName, readOnly string
	var wsrepState int

	if _, err := os.Stat(*forceUpFile); err == nil {
		fmt.Fprint(w, "Cluster node OK by manual override\n")
		return
	}

	if _, err := os.Stat(*forceFailFile); err == nil {
		http.Error(w, "Cluster node unavailable by manual override", http.StatusNotFound)
		return
	}

	err := wsrepStmt.QueryRow().Scan(&fieldName, &wsrepState)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if wsrepState == 2 && *availableWhenDonor == true {
		fmt.Fprint(w, "Cluster node in Donor mode\n")
		return
	} else if wsrepState != 4 {
		http.Error(w, "Cluster node is unavailable", http.StatusServiceUnavailable)
		return
	}

	if *availableWhenReadonly == false {
		err = readOnlyStmt.QueryRow().Scan(&fieldName, &readOnly)
		if err != nil {
			http.Error(w, "Unable to determine read only setting", http.StatusInternalServerError)
			return
		} else if readOnly == "ON" {
			http.Error(w, "Cluster node is read only", http.StatusServiceUnavailable)
			return
		}
	}

	fmt.Fprint(w, "Cluster node OK\n")
}

func main() {
	flag.Parse()

	var net string
	var addr string
	if *socket != "" {
		net = "unix"
		addr = *socket
	} else {
		net = "tcp"
		addr = fmt.Sprintf("%s:%d", *host, *port)
	}

	cfg := mysql.Config{
		User:                    *username,
		Passwd:                  *password,
		Net:                     net,
		Addr:                    addr,
		Timeout:                 *timeout,
		AllowCleartextPasswords: *allowCleartextPasswords,
		AllowNativePasswords:    true,
	}
	dsn := cfg.FormatDSN()

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err.Error())
	}

	db.SetMaxIdleConns(10)

	readOnlyStmt, err = db.Prepare("show global variables like 'read_only'")
	if err != nil {
		log.Fatal(err)
	}

	wsrepStmt, err = db.Prepare("show global status like 'wsrep_local_state'")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Listening...")
	http.HandleFunc("/", checkHandler)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%d", *bindAddr, *bindPort), nil))
}
