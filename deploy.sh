#!/bin/bash

# Cloud Run deployment script
# Usage: ./deploy.sh PROJECT_ID REGION CONNECTOR_NAME [REPOSITORY]

set -e

PROJECT_ID=${1:-"your-project-id"}
REGION=${2:-"us-central1"}
CONNECTOR_NAME=${3:-"vpc-connector"}
REPOSITORY=${4:-"docker-repo"}

echo "Building and deploying to Cloud Run..."
echo "Project: $PROJECT_ID"
echo "Region: $REGION"
echo "VPC Connector: $CONNECTOR_NAME"
echo "Artifact Registry Repository: $REPOSITORY"

# Set the project
gcloud config set project $PROJECT_ID

# Create Artifact Registry repository if it doesn't exist
if ! gcloud artifacts repositories describe $REPOSITORY --location=$REGION --format="value(name)" 2>/dev/null; then
  echo "Creating Artifact Registry repository..."
  gcloud artifacts repositories create $REPOSITORY \
    --repository-format=docker \
    --location=$REGION \
    --description="Docker repository for room-mapping-cache"
fi

# Build and push the image
gcloud builds submit --config=cloudbuild.yaml \
  --substitutions=_REGION=$REGION,_REPOSITORY=$REPOSITORY

# Update service.yaml with actual values
sed -i.bak "s|PROJECT_ID|$PROJECT_ID|g" service.yaml
sed -i.bak "s|REGION|$REGION|g" service.yaml
sed -i.bak "s|CONNECTOR_NAME|$CONNECTOR_NAME|g" service.yaml
sed -i.bak "s|REPOSITORY|$REPOSITORY|g" service.yaml

# Deploy to Cloud Run
gcloud run services replace service.yaml --region=$REGION

# Restore original service.yaml
mv service.yaml.bak service.yaml

echo "Deployment complete!"
echo "Service URL: $(gcloud run services describe room-mapping-cache --region=$REGION --format='value(status.url)')"

