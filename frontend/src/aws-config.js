// AWS Configuration
// Updated with real Terraform output values

export const awsConfig = {
  // AWS region
  region: import.meta.env.VITE_AWS_REGION || 'us-east-1',

  // Cognito User Pool configuration
  userPoolId: import.meta.env.VITE_COGNITO_USER_POOL_ID || 'us-east-1_lgg1jfy8',
  userPoolWebClientId: import.meta.env.VITE_COGNITO_CLIENT_ID || '4k0aml57iukkg5b784jjau79v9',
  identityPoolId: import.meta.env.VITE_COGNITO_IDENTITY_POOL_ID || 'us-east-1:0de49297-1138-48ea-96b3-77eb6522a2c0',

  // API configuration
  apiGatewayUrl: import.meta.env.VITE_API_GATEWAY_URL || 'https://qjs4ux4kyd.execute-api.us-east-1.amazonaws.com/dev',
  websocketUrl: import.meta.env.VITE_WEBSOCKET_URL || 'wss://wnd0jy3rp4.execute-api.us-east-1.amazonaws.com/dev',

  // Cognito domain for hosted UI
  cognitoDomain: import.meta.env.VITE_COGNITO_DOMAIN || 'sphere-of-influence-dev-lgg1jfy8.auth.us-east-1.amazoncognito.com',

  // OAuth configuration
  oauth: {
  domain: import.meta.env.VITE_COGNITO_DOMAIN || 'sphere-of-influence-dev-lgg1jfy8.auth.us-east-1.amazoncognito.com',
  scopes: ['email', 'openid', 'profile'],
  redirectSignIn: [import.meta.env.VITE_COGNITO_CALLBACK_URL || `${window.location.origin}/auth/callback`],
  redirectSignOut: [import.meta.env.VITE_COGNITO_LOGOUT_URL || window.location.origin],
    responseType: 'code'
  }
};

// Log configuration in non-production environments for debugging
// no-op: avoid logging config in client bundles

// Development fallback configuration
if (import.meta.env.DEV && import.meta.env.VITE_USE_AWS_AUTH !== 'true') {
  // Use localhost for development
  if (!awsConfig.apiGatewayUrl) {
    awsConfig.apiGatewayUrl = 'http://localhost:8080/api';
  }
  if (!awsConfig.websocketUrl) {
    awsConfig.websocketUrl = 'ws://localhost:8080/ws';
  }
}

export default awsConfig;