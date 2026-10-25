// Package grpc adapts the ShortenerService protobuf surface onto the same
// service.Shortener the HTTP layer uses.
//
// The typed stubs are generated from api/proto/shortener/v1/shortener.proto
// into pkg/api/v1 and committed, so a plain `go build` works; run `make proto`
// to regenerate them after changing the .proto.
package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/ISch-1/url-shortener/pkg/api/v1"

	"github.com/ISch-1/url-shortener/internal/domain"
	"github.com/ISch-1/url-shortener/internal/service"
)

// Server implements pb.ShortenerServiceServer over the app service.
type Server struct {
	pb.UnimplementedShortenerServiceServer
	svc *service.Shortener
}

func New(svc *service.Shortener) *Server {
	return &Server{svc: svc}
}

func (s *Server) CreateLink(ctx context.Context, r *pb.CreateLinkRequest) (*pb.CreateLinkResponse, error) {
	in := service.CreateInput{OriginalURL: r.GetOriginalUrl(), APIKey: r.GetApiKey()}
	if r.ExpiresAt != nil {
		t := r.ExpiresAt.AsTime()
		in.ExpiresAt = &t
	}
	link, err := s.svc.Create(ctx, in)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidURL) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.CreateLinkResponse{Link: toPBLink(link)}, nil
}

func (s *Server) GetLink(ctx context.Context, r *pb.GetLinkRequest) (*pb.Link, error) {
	link, err := s.svc.Get(ctx, r.GetCode())
	if err != nil {
		if errors.Is(err, domain.ErrLinkNotFound) {
			return nil, status.Error(codes.NotFound, "link not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toPBLink(link), nil
}

func (s *Server) ResolveCode(ctx context.Context, r *pb.ResolveCodeRequest) (*pb.ResolveCodeResponse, error) {
	url, found, err := s.svc.Resolve(ctx, r.GetCode())
	if err != nil {
		if errors.Is(err, domain.ErrLinkExpired) || errors.Is(err, domain.ErrLinkInactive) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.ResolveCodeResponse{OriginalUrl: url, Found: found}, nil
}

func (s *Server) DeactivateLink(ctx context.Context, r *pb.DeactivateLinkRequest) (*pb.DeactivateLinkResponse, error) {
	removed, err := s.svc.Deactivate(ctx, r.GetCode())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.DeactivateLinkResponse{Removed: removed}, nil
}

func toPBLink(l domain.Link) *pb.Link {
	pbLink := &pb.Link{
		Code:        l.Code,
		OriginalUrl: l.OriginalURL,
		SnowflakeId: l.SnowflakeID,
		CreatedAt:   timestamppb.New(l.CreatedAt),
		Active:      l.Active,
		ClickCount:  l.ClickCount,
	}
	if l.ExpiresAt != nil {
		pbLink.ExpiresAt = timestamppb.New(*l.ExpiresAt)
	}
	return pbLink
}
