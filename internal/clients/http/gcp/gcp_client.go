package gcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	guuid "github.com/google/uuid"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/RHEnVision/provisioning-backend/internal/clients"
	"github.com/RHEnVision/provisioning-backend/internal/config"
	"github.com/RHEnVision/provisioning-backend/internal/ctxval"
	"github.com/RHEnVision/provisioning-backend/internal/ptr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type gcpClient struct {
	auth    *clients.Authentication
	options []option.ClientOption
}

func init() {
	clients.GetGCPClient = newGCPClient
}

const TraceName = "github.com/RHEnVision/provisioning-backend/internal/clients/http/gcp"

// GCP SDK does not provide a single client, so only configuration can be shared and
// clients need to be created and closed in each function.
// The difference between the customer and service authentication is which Project ID was given: the service or the customer
func newGCPClient(ctx context.Context, auth *clients.Authentication) (clients.GCP, error) {
	options := []option.ClientOption{
		option.WithCredentialsJSON([]byte(config.GCP.JSON)),
		option.WithQuotaProject(auth.Payload),
		option.WithRequestReason(ctxval.TraceId(ctx)),
	}
	return &gcpClient{
		auth:    auth,
		options: options,
	}, nil
}

func (c *gcpClient) Status(ctx context.Context) error {
	_, err := c.ListAllRegions(ctx)
	return err
}

func (c *gcpClient) ListAllRegions(ctx context.Context) ([]clients.Region, error) {
	ctx, span := otel.Tracer(TraceName).Start(ctx, "ListAllRegions")
	defer span.End()

	client, err := compute.NewRegionsRESTClient(ctx, c.options...)
	if err != nil {
		return nil, fmt.Errorf("unable to create GCP regions client: %w", err)
	}
	defer client.Close()

	// This request returns approx. 6kB response (gzipped). Although REST API allows allow-list
	// of fields via the 'fields' URL param ("items.name,items.zones"), gRPC does not allow that.
	// Therefore, we must download all the information only to extract region and zone names.
	req := &computepb.ListRegionsRequest{
		Project: c.auth.Payload,
	}
	iter := client.List(ctx, req)
	regions := make([]clients.Region, 0, 32)
	for {
		region, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("iterator error: %w", err)
		}
		regions = append(regions, clients.Region(*region.Name))
	}
	return regions, nil
}

func (c *gcpClient) newInstancesClient(ctx context.Context) (*compute.InstancesClient, error) {
	client, err := compute.NewInstancesRESTClient(ctx, c.options...)
	if err != nil {
		return nil, fmt.Errorf("unable to create GCP regions client: %w", err)
	}
	return client, nil
}

func (c *gcpClient) InsertInstances(ctx context.Context, namePattern *string, imageName *string, amount int64, machineType, zone, keyBody string) ([]*string, *string, error) {
	ctx, span := otel.Tracer(TraceName).Start(ctx, "InsertInstances")
	defer span.End()

	logger := logger(ctx)
	logger.Trace().Msgf("Executing bulk insert for name: %s", *namePattern)

	client, err := c.newInstancesClient(ctx)
	if err != nil {
		logger.Error().Err(err).Msgf("Could not get instances client: %v", err)
		return nil, nil, fmt.Errorf("unable to bulk insert instances: %w", err)
	}
	defer client.Close()

	if zone == "" {
		zone = config.GCP.DefaultZone
	}

	uuid := guuid.New().String()
	req := &computepb.BulkInsertInstanceRequest{
		Project: c.auth.Payload,
		Zone:    zone,
		BulkInsertInstanceResourceResource: &computepb.BulkInsertInstanceResource{
			NamePattern: namePattern,
			Count:       &amount,
			MinCount:    &amount,
			InstanceProperties: &computepb.InstanceProperties{
				Labels: map[string]string{
					"rhhcc-rid": uuid,
				},
				Disks: []*computepb.AttachedDisk{
					{
						InitializeParams: &computepb.AttachedDiskInitializeParams{
							SourceImage: imageName,
						},
						AutoDelete: ptr.To(true),
						Boot:       ptr.To(true),
						Type:       ptr.To(computepb.AttachedDisk_PERSISTENT.String()),
					},
				},
				MachineType: ptr.To(machineType),
				NetworkInterfaces: []*computepb.NetworkInterface{
					{
						Name: ptr.To("global/networks/default"),
					},
				},
				Metadata: &computepb.Metadata{
					Items: []*computepb.Items{
						{
							Key:   ptr.To("ssh-keys"),
							Value: ptr.To(keyBody),
						},
					},
				},
			},
		},
	}

	op, err := client.BulkInsert(ctx, req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		logger.Error().Err(err).Msgf("Bulk insert operation has failed: %v", err)
		return nil, nil, fmt.Errorf("cannot bulk insert instances: %w", err)
	}
	if err := op.Wait(ctx); err != nil {
		logger.Error().Err(err).Msgf("Bulk insert operation has failed: %v", err)
		span.SetStatus(codes.Error, err.Error())
		return nil, nil, fmt.Errorf("cannot bulk insert instances: %w", err)
	}

	filter := fmt.Sprintf("labels.rhhcc-id=%v", uuid)
	lstReq := &computepb.AggregatedListInstancesRequest{
		Project: c.auth.Payload,
		Filter:  &filter,
	}

	if !op.Done() {
		return nil, ptr.To(op.Name()), fmt.Errorf("an error occured on operation %s: %w", op.Name(), ErrOperationFailed)
	}

	ids := make([]*string, 0)
	instances := client.AggregatedList(ctx, lstReq)
	for {
		pair, err := instances.Next()
		if errors.Is(err, iterator.Done) {
			logger.Error().Err(err).Msg("Instances iterator has finished")
			break
		} else if err != nil {
			logger.Error().Err(err).Msgf("An error occured during fetching instance ids %s", err.Error())
			span.SetStatus(codes.Error, err.Error())
			return nil, nil, fmt.Errorf("cannot fetch instance ids: %w", err)
		} else {
			logger.Trace().Msg("Fetching instance ids")
			instances := pair.Value.Instances
			for _, o := range instances {
				idAsString := strconv.FormatUint(o.GetId(), 10)
				ids = append(ids, &idAsString)
			}
		}
	}
	return ids, ptr.To(op.Name()), nil
}
