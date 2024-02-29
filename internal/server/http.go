package server

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type Server struct {
	log *Log
}

func New(log *Log) *Server {
	if log == nil {
		panic("nil log")
	}
	return &Server{log: log}
}

func (s *Server) HandleLog(path string) http.Handler {
	r := http.NewServeMux()
	r.HandleFunc("POST "+path, s.ProduceHandler)
	r.HandleFunc("GET "+path, s.ConsumeHandler)
	return r
}

type ProduceRequest struct {
	Record `json:"record"`
}

type ProduceResponse struct {
	Offset uint64 `json:"offset"`
}

func (s *Server) ProduceHandler(w http.ResponseWriter, r *http.Request) {
	var req ProduceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	offset, err := s.log.Append(req.Record)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	res := ProduceResponse{Offset: offset}
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type ConsumeResponse struct {
	Record `json:"record"`
}

func (s *Server) ConsumeHandler(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	offset, _ := strconv.ParseUint(v.Get("offset"), 10, 64)

	record, err := s.log.Read(offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	res := ConsumeResponse{Record: record}
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
