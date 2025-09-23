// AWS Configuration
// Replace these values with your Terraform outputs after deployment

export const awsConfig = {
  // AWS region
  region: process.env.VITE_AWS_REGION || 'us-east-1',
  
  // Cognito User Pool configuration
  userPoolId: process.env.VITE_COGNITO_USER_POOL_ID || '',
  userPoolWebClientId: process.env.VITE_COGNITO_CLIENT_ID || '',
  identityPoolId: process.env.VITE_COGNITO_IDENTITY_POOL_ID || '',
  
  // API configuration
  apiGatewayUrl: process.env.VITE_API_GATEWAY_URL || '',
  websocketUrl: process.env.VITE_WEBSOCKET_URL || '',
  
  // Cognito domain for hosted UI
  cognitoDomain: process.env.VITE_COGNITO_DOMAIN || '',
  
  // OAuth configuration
  oauth: {
    domain: process.env.VITE_COGNITO_DOMAIN || '',
    scope: ['email', 'openid', 'profile'],
    redirectSignIn: process.env.VITE_COGNITO_CALLBACK_URL || window.location.origin,
    redirectSignOut: process.env.VITE_COGNITO_LOGOUT_URL || window.location.origin,
    responseType: 'code'
  }
};

// Development fallback configuration
if (import.meta.env.DEV) {
  // Use localhost for development
  if (!awsConfig.apiGatewayUrl) {
    awsConfig.apiGatewayUrl = 'http://localhost:8080/api';
  }
  if (!awsConfig.websocketUrl) {
    awsConfig.websocketUrl = 'ws://localhost:8080/ws';
  }
}

export default awsConfig;
