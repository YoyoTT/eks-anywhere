// Code generated by smithy-go-codegen DO NOT EDIT.

package snowballdevice

import (
	"context"
	"fmt"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/eks-anywhere/internal/aws-sdk-go-v2/service/snowballdevice/types"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

func (c *Client) DescribeVirtualNetworkInterfaces(ctx context.Context, params *DescribeVirtualNetworkInterfacesInput, optFns ...func(*Options)) (*DescribeVirtualNetworkInterfacesOutput, error) {
	if params == nil {
		params = &DescribeVirtualNetworkInterfacesInput{}
	}

	result, metadata, err := c.invokeOperation(ctx, "DescribeVirtualNetworkInterfaces", params, optFns, c.addOperationDescribeVirtualNetworkInterfacesMiddlewares)
	if err != nil {
		return nil, err
	}

	out := result.(*DescribeVirtualNetworkInterfacesOutput)
	out.ResultMetadata = metadata
	return out, nil
}

type DescribeVirtualNetworkInterfacesInput struct {
	NextToken *string

	noSmithyDocumentSerde
}

type DescribeVirtualNetworkInterfacesOutput struct {

	// This member is required.
	VirtualNetworkInterfaces []types.VirtualNetworkInterface

	NextToken *string

	// Metadata pertaining to the operation's result.
	ResultMetadata middleware.Metadata

	noSmithyDocumentSerde
}

func (c *Client) addOperationDescribeVirtualNetworkInterfacesMiddlewares(stack *middleware.Stack, options Options) (err error) {
	err = stack.Serialize.Add(&awsAwsjson11_serializeOpDescribeVirtualNetworkInterfaces{}, middleware.After)
	if err != nil {
		return err
	}
	err = stack.Deserialize.Add(&awsAwsjson11_deserializeOpDescribeVirtualNetworkInterfaces{}, middleware.After)
	if err != nil {
		return err
	}
	if err = addSetLoggerMiddleware(stack, options); err != nil {
		return err
	}
	if err = awsmiddleware.AddClientRequestIDMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddComputeContentLengthMiddleware(stack); err != nil {
		return err
	}
	if err = addResolveEndpointMiddleware(stack, options); err != nil {
		return err
	}
	if err = v4.AddComputePayloadSHA256Middleware(stack); err != nil {
		return err
	}
	if err = addRetryMiddlewares(stack, options); err != nil {
		return err
	}
	if err = addHTTPSignerV4Middleware(stack, options); err != nil {
		return err
	}
	if err = awsmiddleware.AddRawResponseToMetadata(stack); err != nil {
		return err
	}
	if err = awsmiddleware.AddRecordResponseTiming(stack); err != nil {
		return err
	}
	if err = addClientUserAgent(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddErrorCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = stack.Initialize.Add(newServiceMetadataMiddleware_opDescribeVirtualNetworkInterfaces(options.Region), middleware.Before); err != nil {
		return err
	}
	if err = addRequestIDRetrieverMiddleware(stack); err != nil {
		return err
	}
	if err = addResponseErrorMiddleware(stack); err != nil {
		return err
	}
	if err = addRequestResponseLogging(stack, options); err != nil {
		return err
	}
	return nil
}

// DescribeVirtualNetworkInterfacesAPIClient is a client that implements the
// DescribeVirtualNetworkInterfaces operation.
type DescribeVirtualNetworkInterfacesAPIClient interface {
	DescribeVirtualNetworkInterfaces(context.Context, *DescribeVirtualNetworkInterfacesInput, ...func(*Options)) (*DescribeVirtualNetworkInterfacesOutput, error)
}

var _ DescribeVirtualNetworkInterfacesAPIClient = (*Client)(nil)

// DescribeVirtualNetworkInterfacesPaginatorOptions is the paginator options for
// DescribeVirtualNetworkInterfaces
type DescribeVirtualNetworkInterfacesPaginatorOptions struct {
	// Set to true if pagination should stop if the service returns a pagination token
	// that matches the most recent token provided to the service.
	StopOnDuplicateToken bool
}

// DescribeVirtualNetworkInterfacesPaginator is a paginator for
// DescribeVirtualNetworkInterfaces
type DescribeVirtualNetworkInterfacesPaginator struct {
	options   DescribeVirtualNetworkInterfacesPaginatorOptions
	client    DescribeVirtualNetworkInterfacesAPIClient
	params    *DescribeVirtualNetworkInterfacesInput
	nextToken *string
	firstPage bool
}

// NewDescribeVirtualNetworkInterfacesPaginator returns a new
// DescribeVirtualNetworkInterfacesPaginator
func NewDescribeVirtualNetworkInterfacesPaginator(client DescribeVirtualNetworkInterfacesAPIClient, params *DescribeVirtualNetworkInterfacesInput, optFns ...func(*DescribeVirtualNetworkInterfacesPaginatorOptions)) *DescribeVirtualNetworkInterfacesPaginator {
	if params == nil {
		params = &DescribeVirtualNetworkInterfacesInput{}
	}

	options := DescribeVirtualNetworkInterfacesPaginatorOptions{}

	for _, fn := range optFns {
		fn(&options)
	}

	return &DescribeVirtualNetworkInterfacesPaginator{
		options:   options,
		client:    client,
		params:    params,
		firstPage: true,
		nextToken: params.NextToken,
	}
}

// HasMorePages returns a boolean indicating whether more pages are available
func (p *DescribeVirtualNetworkInterfacesPaginator) HasMorePages() bool {
	return p.firstPage || (p.nextToken != nil && len(*p.nextToken) != 0)
}

// NextPage retrieves the next DescribeVirtualNetworkInterfaces page.
func (p *DescribeVirtualNetworkInterfacesPaginator) NextPage(ctx context.Context, optFns ...func(*Options)) (*DescribeVirtualNetworkInterfacesOutput, error) {
	if !p.HasMorePages() {
		return nil, fmt.Errorf("no more pages available")
	}

	params := *p.params
	params.NextToken = p.nextToken

	result, err := p.client.DescribeVirtualNetworkInterfaces(ctx, &params, optFns...)
	if err != nil {
		return nil, err
	}
	p.firstPage = false

	prevToken := p.nextToken
	p.nextToken = result.NextToken

	if p.options.StopOnDuplicateToken &&
		prevToken != nil &&
		p.nextToken != nil &&
		*prevToken == *p.nextToken {
		p.nextToken = nil
	}

	return result, nil
}

func newServiceMetadataMiddleware_opDescribeVirtualNetworkInterfaces(region string) *awsmiddleware.RegisterServiceMetadata {
	return &awsmiddleware.RegisterServiceMetadata{
		Region:        region,
		ServiceID:     ServiceID,
		SigningName:   "snowballdevice",
		OperationName: "DescribeVirtualNetworkInterfaces",
	}
}
