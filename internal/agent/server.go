package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"time"

	"github.com/amir20/dozzle/internal/agent/pb"
	"github.com/amir20/dozzle/internal/docker"
	orderedmap "github.com/wk8/go-ordered-map/v2"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"google.golang.org/grpc/status"
)

type server struct {
	client  docker.Client
	store   *docker.ContainerStore
	version string

	pb.UnimplementedAgentServiceServer
}

func newServer(client docker.Client, dozzleVersion string) pb.AgentServiceServer {
	return &server{
		client:  client,
		version: dozzleVersion,

		store: docker.NewContainerStore(context.Background(), client),
	}
}

func (s *server) StreamLogs(in *pb.StreamLogsRequest, out pb.AgentService_StreamLogsServer) error {
	since := time.Time{}
	if in.Since != nil {
		since = in.Since.AsTime()
	}

	reader, err := s.client.ContainerLogs(out.Context(), in.ContainerId, since, docker.StdType(in.StreamTypes))
	if err != nil {
		return err
	}

	container, err := s.store.FindContainer(in.ContainerId)
	if err != nil {
		return err
	}

	g := docker.NewEventGenerator(out.Context(), reader, container)

	for event := range g.Events {
		out.Send(&pb.StreamLogsResponse{
			Event: logEventToPb(event),
		})
	}

	select {
	case e := <-g.Errors:
		return e
	default:
		return nil
	}
}

func (s *server) LogsBetweenDates(in *pb.LogsBetweenDatesRequest, out pb.AgentService_LogsBetweenDatesServer) error {
	reader, err := s.client.ContainerLogsBetweenDates(out.Context(), in.ContainerId, in.Since.AsTime(), in.Until.AsTime(), docker.StdType(in.StreamTypes))
	if err != nil {
		return err
	}

	container, err := s.client.FindContainer(in.ContainerId)
	if err != nil {
		return err
	}

	g := docker.NewEventGenerator(out.Context(), reader, container)

	for {
		select {
		case event := <-g.Events:
			out.Send(&pb.StreamLogsResponse{
				Event: logEventToPb(event),
			})
		case e := <-g.Errors:
			return e
		case <-out.Context().Done():
			return nil
		}
	}
}

func (s *server) StreamRawBytes(in *pb.StreamRawBytesRequest, out pb.AgentService_StreamRawBytesServer) error {
	reader, err := s.client.ContainerLogsBetweenDates(out.Context(), in.ContainerId, in.Since.AsTime(), in.Until.AsTime(), docker.StdType(in.StreamTypes))

	if err != nil {
		return err
	}

	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			return err
		}

		if n == 0 {
			break
		}

		if err := out.Send(&pb.StreamRawBytesResponse{
			Data: buf[:n],
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *server) StreamEvents(in *pb.StreamEventsRequest, out pb.AgentService_StreamEventsServer) error {
	events := make(chan docker.ContainerEvent)

	s.store.SubscribeEvents(out.Context(), events)

	for {
		select {
		case event := <-events:
			out.Send(&pb.StreamEventsResponse{
				Event: &pb.ContainerEvent{
					ActorId: event.ActorID,
					Name:    event.Name,
					Host:    event.Host,
				},
			})
		case <-out.Context().Done():
			return nil
		}
	}
}

func (s *server) StreamStats(in *pb.StreamStatsRequest, out pb.AgentService_StreamStatsServer) error {
	stats := make(chan docker.ContainerStat)

	s.store.SubscribeStats(out.Context(), stats)

	for {
		select {
		case stat := <-stats:
			out.Send(&pb.StreamStatsResponse{
				Stat: &pb.ContainerStat{
					Id:            stat.ID,
					CpuPercent:    stat.CPUPercent,
					MemoryPercent: stat.MemoryPercent,
					MemoryUsage:   stat.MemoryUsage,
				},
			})
		case <-out.Context().Done():
			return nil
		}
	}
}

func (s *server) FindContainer(ctx context.Context, in *pb.FindContainerRequest) (*pb.FindContainerResponse, error) {
	container, err := s.store.FindContainer(in.ContainerId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &pb.FindContainerResponse{
		Container: &pb.Container{
			Id:      container.ID,
			Name:    container.Name,
			Image:   container.Image,
			ImageId: container.ImageID,
			Command: container.Command,
			Created: timestamppb.New(container.Created),
			State:   container.State,
			Health:  container.Health,
			Host:    container.Host,
			Tty:     container.Tty,
			Labels:  container.Labels,
			Group:   container.Group,
			Started: timestamppb.New(container.StartedAt),
		},
	}, nil
}

func (s *server) ListContainers(ctx context.Context, in *pb.ListContainersRequest) (*pb.ListContainersResponse, error) {
	containers, err := s.store.ListContainers()
	if err != nil {
		return nil, err
	}

	var pbContainers []*pb.Container

	for _, container := range containers {
		var pbStats []*pb.ContainerStat
		for _, stat := range container.Stats.Data() {
			pbStats = append(pbStats, &pb.ContainerStat{
				Id:            stat.ID,
				CpuPercent:    stat.CPUPercent,
				MemoryPercent: stat.MemoryPercent,
				MemoryUsage:   stat.MemoryUsage,
			})
		}

		pbContainers = append(pbContainers, &pb.Container{
			Id:      container.ID,
			Name:    container.Name,
			Image:   container.Image,
			ImageId: container.ImageID,
			Created: timestamppb.New(container.Created),
			State:   container.State,
			Health:  container.Health,
			Host:    container.Host,
			Tty:     container.Tty,
			Labels:  container.Labels,
			Group:   container.Group,
			Started: timestamppb.New(container.StartedAt),
			Stats:   pbStats,
			Command: container.Command,
		})
	}

	return &pb.ListContainersResponse{
		Containers: pbContainers,
	}, nil
}

func (s *server) HostInfo(ctx context.Context, in *pb.HostInfoRequest) (*pb.HostInfoResponse, error) {
	host := s.client.Host()
	return &pb.HostInfoResponse{
		Host: &pb.Host{
			Id:            host.ID,
			Name:          host.Name,
			CpuCores:      uint32(host.NCPU),
			Memory:        uint64(host.MemTotal),
			DockerVersion: host.DockerVersion,
			AgentVersion:  s.version,
		},
	}, nil
}

func (s *server) StreamContainerStarted(in *pb.StreamContainerStartedRequest, out pb.AgentService_StreamContainerStartedServer) error {
	containers := make(chan docker.Container)

	go s.store.SubscribeNewContainers(out.Context(), containers)

	for {
		select {
		case container := <-containers:
			out.Send(&pb.StreamContainerStartedResponse{
				Container: &pb.Container{
					Id:      container.ID,
					Name:    container.Name,
					Image:   container.Image,
					ImageId: container.ImageID,
					Created: timestamppb.New(container.Created),
					State:   container.State,
					Health:  container.Health,
					Host:    container.Host,
					Tty:     container.Tty,
					Labels:  container.Labels,
					Group:   container.Group,
					Started: timestamppb.New(container.StartedAt),
				},
			})
		case <-out.Context().Done():
			return nil
		}
	}
}

func (s *server) ContainerAction(ctx context.Context, in *pb.ContainerActionRequest) (*pb.ContainerActionResponse, error) {
	var action docker.ContainerAction
	switch in.Action {
	case pb.ContainerAction_Start:
		action = docker.Start

	case pb.ContainerAction_Stop:
		action = docker.Stop

	case pb.ContainerAction_Restart:
		action = docker.Restart

	default:
		return nil, status.Error(codes.InvalidArgument, "invalid action")
	}

	err := s.client.ContainerActions(action, in.ContainerId)

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.ContainerActionResponse{}, nil
}

func NewServer(client docker.Client, certificates tls.Certificate, dozzleVersion string) *grpc.Server {
	caCertPool := x509.NewCertPool()
	c, err := x509.ParseCertificate(certificates.Certificate[0])
	if err != nil {
		log.Fatalf("failed to parse certificate: %v", err)
	}
	caCertPool.AddCert(c)

	// Create the TLS configuration
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certificates},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert, // Require client certificates
	}

	// Create the gRPC server with the credentials
	creds := credentials.NewTLS(tlsConfig)

	grpcServer := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterAgentServiceServer(grpcServer, newServer(client, dozzleVersion))

	return grpcServer
}

func logEventToPb(event *docker.LogEvent) *pb.LogEvent {
	var message *anypb.Any
	switch data := event.Message.(type) {
	case string:
		message, _ = anypb.New(&pb.SimpleMessage{
			Message: data,
		})

	case *orderedmap.OrderedMap[string, any]:
		message, _ = anypb.New(&pb.ComplexMessage{
			Data: orderedMapToJSONBytes(data),
		})
	case *orderedmap.OrderedMap[string, string]:
		message, _ = anypb.New(&pb.ComplexMessage{
			Data: orderedMapToJSONBytes(data),
		})

	default:
		log.Fatalf("agent server: unknown type %T", event.Message)
	}

	return &pb.LogEvent{
		Message:     message,
		Timestamp:   timestamppb.New(time.Unix(event.Timestamp, 0)),
		Id:          event.Id,
		ContainerId: event.ContainerID,
		Level:       event.Level,
		Stream:      event.Stream,
		Position:    string(event.Position),
	}
}

func orderedMapToJSONBytes[T any](data *orderedmap.OrderedMap[string, T]) []byte {
	bytes := bytes.Buffer{}
	json.NewEncoder(&bytes).Encode(data)
	return bytes.Bytes()
}
