package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/iancoleman/strcase"
	"github.com/kshvakov/clickhouse"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"time"
)

var connect *sql.DB

func main() {
	var err error
	connect, err = sql.Open("clickhouse", "tcp://localhost:9001?database=ds&debug=true")
	sqlString := createTable()
	_, err = execQuery(sqlString)
	handleError(err)
	handler := http.NewServeMux()
	handler.HandleFunc("/query", Logger(selectQuery))
	handler.HandleFunc("/addEvent", Logger(addEvent))
	handler.HandleFunc("/", welcomePage)
	s := http.Server{
		Addr:           "0.0.0.0:1234",
		Handler:        handler,
		//ReadTimeout:    1000 * time.Second,
		//WriteTimeout:   1000 * time.Second,
		//IdleTimeout:    0 * time.Second,
		//MaxHeaderBytes: 1 << 20, //1*2^20 - 128 kByte
	}
	log.Println(s.ListenAndServe())
}

func welcomePage(w http.ResponseWriter, r *http.Request) {
	renderResponse(nil, w,
		`<head>
<link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.3.1/css/bootstrap.min.css" 
integrity="sha384-ggOyR0iXCbMQv3Xipma34MD+dH/1fQ784/j6cY/iJTQUOhcWr7x9JvoRxT2MZw1T" crossorigin="anonymous"></head>
					<div class="text-center">	<h2>Welcome to ClickHouse event tracker API</h2>
						<h5>Please read <a href="https://docs.google.com/document/d/1XsQY9_5Yi7YpYj_b6xQq4Qm7ePc7MNgWv5o-gL7Gjj8/edit?usp=sharing">documentation</a></h5>
					</div>`)
}

type Query struct {
	QueryType string `json:"query_type"`
	Query     string `json:"query"`
}

type LinuxTime int

type Event struct {
	PlayerId      string                 `json:"player_id"`
	PlayerName    string                 `json:"player_name"`
	EventType     string                 `json:"event_type"`
	SessionUid    string                 `json:"session_uid"`
	DateTime      LinuxTime              `json:"date_time"`
	Registered    LinuxTime              `json:"registered"`
	Level         int                    `json:"level"`
	ExpCount      int                    `json:"exp_count"`
	SessionNum    int                    `json:"session_num"`
	SoftBalance   int                    `json:"soft_balance"`
	HardBalance   int                    `json:"hard_balance"`
	ShardsBalance int                    `json:"shards_balance"`
	EventData     map[string]interface{} `json:"event_data"`
}

func createTable() string {
	println(clickhouse.DefaultConnTimeout)
	event := Event{}
	tableName := "ds_event"
	e := reflect.ValueOf(&event).Elem()
	var sql bytes.Buffer
	sql.WriteString("CREATE TABLE IF NOT EXISTS ")
	sql.WriteString(tableName)
	sql.WriteString(" (")

	for i := 0; i < e.NumField(); i++ {
		varName := e.Type().Field(i).Name
		varType := e.Type().Field(i).Type

		sql.WriteString(strcase.ToSnake(varName))
		sql.WriteString(" ")
		sql.WriteString(typeCast(strcase.ToCamel(fmt.Sprintf("%v", varType))))
		if i != e.NumField()-1 {
			sql.WriteString(", ")
		}
	}
	sql.WriteString(") ENGINE = MergeTree PARTITION BY toYYYYMMDD(date_time) ORDER BY date_time")

	return sql.String()
}

func addEvent(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	var event Event
	err = json.Unmarshal(body, &event)
	var stringResult string

	if err == nil {
		stringResult, err = insertEvent(event)
	}
	renderResponse(err, w, stringResult)
}

func insertEvent(event Event) (string, error) {
	e := reflect.ValueOf(&event).Elem()
	var sqlString bytes.Buffer
	var valuesPlaceholder bytes.Buffer
	var values []interface{}
	sqlString.WriteString("INSERT INTO ds_event (")
	valuesPlaceholder.WriteString(" VALUES (")

	for i := 0; i < e.NumField(); i++ {
		varName := strcase.ToSnake(e.Type().Field(i).Name)
		varType := strcase.ToCamel(fmt.Sprintf("%v", e.Type().Field(i).Type))
		varValue := castValueByType(varType, e.Field(i).Interface())

		values = append(values, varValue)
		sqlString.WriteString(varName)
		valuesPlaceholder.WriteString("?")
		if i != e.NumField()-1 {
			sqlString.WriteString(", ")
			valuesPlaceholder.WriteString(", ")
		}
	}
	sqlString.WriteString(")")
	valuesPlaceholder.WriteString(")")
	sqlString.WriteString(valuesPlaceholder.String())

	var (
		tx, _   = connect.Begin()
		stmt, _ = tx.Prepare(sqlString.String())
	)
	defer stmt.Close()
	_, err := stmt.Exec(values...)
	if err != nil {

		return "", err
	}
	err = tx.Commit()

	return `{"status":"ok"}`, nil
}

func typeCast(structType string) string {
	switch structType {
	case "MainLinuxTime":
		return "DateTime"
	case "Mapstringinterface":
		return "String"
	default:
		return structType
	}
}

func castValueByType(structType string, value interface{}) interface{} {
	switch structType {
	case "MainLinuxTime":
		return time.Unix(int64(value.(LinuxTime)), 0)
	case "Mapstringinterface":
		value, _ = json.Marshal(value)

		return value
	default:
		return value
	}
}

func selectQuery(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	var params Query
	err = json.Unmarshal(body, &params)
	var stringResult string
	switch params.QueryType {
	case "select":
		stringResult, err = getResultInJson(params.Query)
	case "exec":
		stringResult, err = execQuery(params.Query)
	default:
		err = errors.New("undefined query_type")
	}
	renderResponse(err, w, stringResult)
}

func renderResponse(err error, w http.ResponseWriter, stringResult string) {
	err = getJsonError(err)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		_, err = w.Write([]byte(err.Error()))
	} else {
		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte(stringResult))
	}
}

func execQuery(sqlString string) (string, error) {
	_, err := connect.Exec(sqlString)

	if err != nil {
		return "", err
	}

	return `{"status":"ok"}`, nil
}

func Logger(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		log.Printf("Server [http] method [%s] connnection from [%v]", r.Method, r.RemoteAddr)
		next.ServeHTTP(w, r)
	}
}

func getResultInJson(sqlString string) (string, error) {
	rows, err := connect.Query(sqlString)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	count := len(columns)
	tableData := make([]map[string]interface{}, 0)
	values := make([]interface{}, count)
	valuePtrs := make([]interface{}, count)
	for rows.Next() {
		for i := 0; i < count; i++ {
			valuePtrs[i] = &values[i]
		}
		rows.Scan(valuePtrs...)
		entry := make(map[string]interface{})
		for i, col := range columns {
			var v interface{}
			val := values[i]
			b, ok := val.([]byte)
			if ok {
				v = string(b)
			} else {
				v = val
			}
			entry[col] = v
		}
		tableData = append(tableData, entry)
	}
	jsonData, err := json.Marshal(tableData)
	if err != nil {
		return "", err
	}
	//fmt.Println(string(jsonData))
	return string(jsonData), nil
}

func getJsonError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
}

func handleError(error error) {
	if error != nil {
		log.Fatal(error)
	}
}
