package edgestacks

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/asaskevich/govalidator"
	httperror "github.com/portainer/libhttp/error"
	"github.com/portainer/libhttp/request"
	"github.com/portainer/libhttp/response"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/filesystem"
	"github.com/portainer/portainer/api/internal/edge"
)

// @id EdgeStackCreate
// @summary Create an EdgeStack
// @description
// @tags edge_stacks
// @security jwt
// @accept json
// @produce json
// @param method query string true "Creation Method" Enums(file,string,repository)
// @param body_string body swarmStackFromFileContentPayload true "Required when using method=string"
// @param body_file body swarmStackFromFileUploadPayload true "Required when using method=file"
// @param body_repository body swarmStackFromGitRepositoryPayload true "Required when using method=repository"
// @success 200 {object} portainer.EdgeStack
// @failure 500
// @failure 503 Edge compute features are disabled
// @router /edge_stacks [post]
func (handler *Handler) edgeStackCreate(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
	method, err := request.RetrieveQueryParameter(r, "method", false)
	if err != nil {
		return &httperror.HandlerError{http.StatusBadRequest, "Invalid query parameter: method", err}
	}

	edgeStack, err := handler.createSwarmStack(method, r)
	if err != nil {
		return &httperror.HandlerError{http.StatusInternalServerError, "Unable to create Edge stack", err}
	}

	return response.JSON(w, edgeStack)
}

func (handler *Handler) createSwarmStack(method string, r *http.Request) (*portainer.EdgeStack, error) {
	switch method {
	case "string":
		return handler.createSwarmStackFromFileContent(r)
	case "repository":
		return handler.createSwarmStackFromGitRepository(r)
	case "file":
		return handler.createSwarmStackFromFileUpload(r)
	}
	return nil, errors.New("Invalid value for query parameter: method. Value must be one of: string, repository or file")
}

type swarmStackFromFileContentPayload struct {
	// Name of the stack
	Name string `example:"myStack" validate:"required"`
	// Content of the Stack file
	StackFileContent string `example:"version: 3\n services:\n web:\n image:nginx" validate:"required"`
	// List of identifiers of EdgeGroups
	EdgeGroups []portainer.EdgeGroupID `example:"1"`
}

func (payload *swarmStackFromFileContentPayload) Validate(r *http.Request) error {
	if govalidator.IsNull(payload.Name) {
		return errors.New("Invalid stack name")
	}
	if govalidator.IsNull(payload.StackFileContent) {
		return errors.New("Invalid stack file content")
	}
	if payload.EdgeGroups == nil || len(payload.EdgeGroups) == 0 {
		return errors.New("Edge Groups are mandatory for an Edge stack")
	}
	return nil
}

func (handler *Handler) createSwarmStackFromFileContent(r *http.Request) (*portainer.EdgeStack, error) {
	var payload swarmStackFromFileContentPayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return nil, err
	}

	err = handler.validateUniqueName(payload.Name)
	if err != nil {
		return nil, err
	}

	stackID := handler.DataStore.EdgeStack().GetNextIdentifier()
	stack := &portainer.EdgeStack{
		ID:           portainer.EdgeStackID(stackID),
		Name:         payload.Name,
		EntryPoint:   filesystem.ComposeFileDefaultName,
		CreationDate: time.Now().Unix(),
		EdgeGroups:   payload.EdgeGroups,
		Status:       make(map[portainer.EndpointID]portainer.EdgeStackStatus),
		Version:      1,
	}

	stackFolder := strconv.Itoa(int(stack.ID))
	projectPath, err := handler.FileService.StoreEdgeStackFileFromBytes(stackFolder, stack.EntryPoint, []byte(payload.StackFileContent))
	if err != nil {
		return nil, err
	}
	stack.ProjectPath = projectPath

	relationConfig, err := fetchEndpointRelationsConfig(handler.DataStore)
	if err != nil {
		return nil, fmt.Errorf("failed fetching relations config: %w", err)
	}

	relatedEndpointIds, err := edge.EdgeStackRelatedEndpoints(stack.EdgeGroups, relationConfig.endpoints, relationConfig.endpointGroups, relationConfig.edgeGroups)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve related endpoints: %w", err)
	}

	err = handler.convertAndStoreKubeManifestIfNeeded(relatedEndpointIds, stack)
	if err != nil {
		return nil, fmt.Errorf("Failed creating and storing kube manifest: %w", err)
	}

	err = handler.updateEndpointRelations(stack, relatedEndpointIds)
	if err != nil {
		return nil, fmt.Errorf("Unable to update endpoint relations: %w", err)
	}

	err = handler.DataStore.EdgeStack().CreateEdgeStack(stack)
	if err != nil {
		return nil, err
	}

	return stack, nil
}

type swarmStackFromGitRepositoryPayload struct {
	// Name of the stack
	Name string `example:"myStack" validate:"required"`
	// URL of a Git repository hosting the Stack file
	RepositoryURL string `example:"https://github.com/openfaas/faas" validate:"required"`
	// Reference name of a Git repository hosting the Stack file
	RepositoryReferenceName string `example:"refs/heads/master"`
	// Use basic authentication to clone the Git repository
	RepositoryAuthentication bool `example:"true"`
	// Username used in basic authentication. Required when RepositoryAuthentication is true.
	RepositoryUsername string `example:"myGitUsername"`
	// Password used in basic authentication. Required when RepositoryAuthentication is true.
	RepositoryPassword string `example:"myGitPassword"`
	// Path to the Stack file inside the Git repository
	ComposeFilePathInRepository string `example:"docker-compose.yml" default:"docker-compose.yml"`
	// List of identifiers of EdgeGroups
	EdgeGroups []portainer.EdgeGroupID `example:"1"`
}

func (payload *swarmStackFromGitRepositoryPayload) Validate(r *http.Request) error {
	if govalidator.IsNull(payload.Name) {
		return errors.New("Invalid stack name")
	}
	if govalidator.IsNull(payload.RepositoryURL) || !govalidator.IsURL(payload.RepositoryURL) {
		return errors.New("Invalid repository URL. Must correspond to a valid URL format")
	}
	if payload.RepositoryAuthentication && (govalidator.IsNull(payload.RepositoryUsername) || govalidator.IsNull(payload.RepositoryPassword)) {
		return errors.New("Invalid repository credentials. Username and password must be specified when authentication is enabled")
	}
	if govalidator.IsNull(payload.ComposeFilePathInRepository) {
		payload.ComposeFilePathInRepository = filesystem.ComposeFileDefaultName
	}
	if payload.EdgeGroups == nil || len(payload.EdgeGroups) == 0 {
		return errors.New("Edge Groups are mandatory for an Edge stack")
	}
	return nil
}

func (handler *Handler) createSwarmStackFromGitRepository(r *http.Request) (*portainer.EdgeStack, error) {
	var payload swarmStackFromGitRepositoryPayload
	err := request.DecodeAndValidateJSONPayload(r, &payload)
	if err != nil {
		return nil, err
	}

	err = handler.validateUniqueName(payload.Name)
	if err != nil {
		return nil, err
	}

	stackID := handler.DataStore.EdgeStack().GetNextIdentifier()
	stack := &portainer.EdgeStack{
		ID:           portainer.EdgeStackID(stackID),
		Name:         payload.Name,
		EntryPoint:   payload.ComposeFilePathInRepository,
		CreationDate: time.Now().Unix(),
		EdgeGroups:   payload.EdgeGroups,
		Status:       make(map[portainer.EndpointID]portainer.EdgeStackStatus),
		Version:      1,
	}

	projectPath := handler.FileService.GetEdgeStackProjectPath(strconv.Itoa(int(stack.ID)))
	stack.ProjectPath = projectPath

	gitCloneParams := &cloneRepositoryParameters{
		url:            payload.RepositoryURL,
		referenceName:  payload.RepositoryReferenceName,
		path:           projectPath,
		authentication: payload.RepositoryAuthentication,
		username:       payload.RepositoryUsername,
		password:       payload.RepositoryPassword,
	}

	err = handler.cloneGitRepository(gitCloneParams)
	if err != nil {
		return nil, err
	}

	relationConfig, err := fetchEndpointRelationsConfig(handler.DataStore)
	if err != nil {
		return nil, fmt.Errorf("failed fetching relations config: %w", err)
	}

	relatedEndpointIds, err := edge.EdgeStackRelatedEndpoints(stack.EdgeGroups, relationConfig.endpoints, relationConfig.endpointGroups, relationConfig.edgeGroups)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve related endpoints: %w", err)
	}

	err = handler.convertAndStoreKubeManifestIfNeeded(relatedEndpointIds, stack)
	if err != nil {
		return nil, fmt.Errorf("Failed creating and storing kube manifest: %w", err)
	}

	err = handler.updateEndpointRelations(stack, relatedEndpointIds)
	if err != nil {
		return nil, fmt.Errorf("Unable to update endpoint relations: %w", err)
	}

	err = handler.DataStore.EdgeStack().CreateEdgeStack(stack)
	if err != nil {
		return nil, err
	}

	return stack, nil
}

type swarmStackFromFileUploadPayload struct {
	Name             string
	StackFileContent []byte
	EdgeGroups       []portainer.EdgeGroupID
}

func (payload *swarmStackFromFileUploadPayload) Validate(r *http.Request) error {
	name, err := request.RetrieveMultiPartFormValue(r, "Name", false)
	if err != nil {
		return errors.New("Invalid stack name")
	}
	payload.Name = name

	composeFileContent, _, err := request.RetrieveMultiPartFormFile(r, "file")
	if err != nil {
		return errors.New("Invalid Compose file. Ensure that the Compose file is uploaded correctly")
	}
	payload.StackFileContent = composeFileContent

	var edgeGroups []portainer.EdgeGroupID
	err = request.RetrieveMultiPartFormJSONValue(r, "EdgeGroups", &edgeGroups, false)
	if err != nil || len(edgeGroups) == 0 {
		return errors.New("Edge Groups are mandatory for an Edge stack")
	}
	payload.EdgeGroups = edgeGroups
	return nil
}

func (handler *Handler) createSwarmStackFromFileUpload(r *http.Request) (*portainer.EdgeStack, error) {
	payload := &swarmStackFromFileUploadPayload{}
	err := payload.Validate(r)
	if err != nil {
		return nil, err
	}

	err = handler.validateUniqueName(payload.Name)
	if err != nil {
		return nil, err
	}

	stackID := handler.DataStore.EdgeStack().GetNextIdentifier()
	stack := &portainer.EdgeStack{
		ID:           portainer.EdgeStackID(stackID),
		Name:         payload.Name,
		EntryPoint:   filesystem.ComposeFileDefaultName,
		CreationDate: time.Now().Unix(),
		EdgeGroups:   payload.EdgeGroups,
		Status:       make(map[portainer.EndpointID]portainer.EdgeStackStatus),
		Version:      1,
	}

	stackFolder := strconv.Itoa(int(stack.ID))
	projectPath, err := handler.FileService.StoreEdgeStackFileFromBytes(stackFolder, stack.EntryPoint, payload.StackFileContent)
	if err != nil {
		return nil, err
	}
	stack.ProjectPath = projectPath

	relationConfig, err := fetchEndpointRelationsConfig(handler.DataStore)
	if err != nil {
		return nil, fmt.Errorf("failed fetching relations config: %w", err)
	}

	relatedEndpointIds, err := edge.EdgeStackRelatedEndpoints(stack.EdgeGroups, relationConfig.endpoints, relationConfig.endpointGroups, relationConfig.edgeGroups)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve related endpoints: %w", err)
	}

	err = handler.convertAndStoreKubeManifestIfNeeded(relatedEndpointIds, stack)
	if err != nil {
		return nil, fmt.Errorf("Failed creating and storing kube manifest: %w", err)
	}

	err = handler.updateEndpointRelations(stack, relatedEndpointIds)
	if err != nil {
		return nil, fmt.Errorf("Unable to update endpoint relations: %w", err)
	}

	err = handler.DataStore.EdgeStack().CreateEdgeStack(stack)
	if err != nil {
		return nil, err
	}

	return stack, nil
}

func (handler *Handler) validateUniqueName(name string) error {
	edgeStacks, err := handler.DataStore.EdgeStack().EdgeStacks()
	if err != nil {
		return err
	}

	for _, stack := range edgeStacks {
		if strings.EqualFold(stack.Name, name) {
			return errors.New("Edge stack name must be unique")
		}
	}
	return nil
}

func (handler *Handler) updateEndpointRelations(edgeStack *portainer.EdgeStack, relatedEndpointIds []portainer.EndpointID) error {
	for _, endpointID := range relatedEndpointIds {
		relation, err := handler.DataStore.EndpointRelation().EndpointRelation(endpointID)
		if err != nil {
			return fmt.Errorf("unable to find endpoint relation in database: %w", err)
		}

		relation.EdgeStacks[edgeStack.ID] = true

		err = handler.DataStore.EndpointRelation().UpdateEndpointRelation(endpointID, relation)
		if err != nil {
			return fmt.Errorf("unable to persist endpoint relation in database: %w", err)
		}
	}

	return nil
}

func (handler *Handler) convertAndStoreKubeManifestIfNeeded(relatedEndpointIds []portainer.EndpointID, edgeStack *portainer.EdgeStack) error {
	hasKubeEndpoint, err := hasKubeEndpoint(handler.DataStore.Endpoint(), relatedEndpointIds)
	if err != nil {
		return fmt.Errorf("unable to check if edge stack has kube endpoints: %w", err)
	}

	if !hasKubeEndpoint {
		return nil
	}

	composeConfig, err := handler.FileService.GetFileContent(path.Join(edgeStack.ProjectPath, edgeStack.EntryPoint))
	if err != nil {
		return fmt.Errorf("unable to retrieve Compose file from disk: %w", err)
	}

	kompose, err := handler.KubernetesDeployer.ConvertCompose(string(composeConfig))
	if err != nil {
		return fmt.Errorf("failed converting compose file to kubernetes manifest: %w", err)
	}

	KomposeFileName := filesystem.KubeManifestFileDefaultName
	_, err = handler.FileService.StoreEdgeStackFileFromBytes(strconv.Itoa(int(edgeStack.ID)), KomposeFileName, kompose)
	if err != nil {
		return fmt.Errorf("failed to store kube manifest file: %w", err)
	}

	edgeStack.ManifestPath = KomposeFileName

	return nil
}
