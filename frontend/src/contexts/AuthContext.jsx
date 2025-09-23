import React, { createContext, useContext, useEffect, useState } from 'react';
import { Amplify } from 'aws-amplify';
import { getCurrentUser, signIn, signOut, signUp, confirmSignUp, resendSignUpCode, fetchAuthSession } from 'aws-amplify/auth';
import awsConfig from '../aws-config.js';

// Check if we're in development mode without AWS configuration
const isDevMode = import.meta.env.DEV && (!awsConfig.userPoolId || !awsConfig.userPoolWebClientId);

// Configure Amplify only if we have valid AWS configuration
if (!isDevMode) {
  Amplify.configure({
    Auth: {
      Cognito: {
        region: awsConfig.region,
        userPoolId: awsConfig.userPoolId,
        userPoolClientId: awsConfig.userPoolWebClientId,
        identityPoolId: awsConfig.identityPoolId,
        loginWith: {
          oauth: awsConfig.oauth,
          username: true,
          email: true
        }
      }
    }
  });
}

const AuthContext = createContext({});

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};

export const AuthProvider = ({ children }) => {
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(true);

  // Mock functions for development mode
  const mockGetAccessToken = async () => {
    return 'mock-jwt-token';
  };

  const mockCheckAuthState = async () => {
    // In dev mode, automatically sign in with a mock user
    const mockUser = {
      username: 'devuser',
      email: 'dev@example.com',
      name: 'Development User',
      sub: 'dev-user-123'
    };
    setUser(mockUser);
    setLoading(false);
  };

  const mockSignIn = async (username, password) => {
    const mockUser = {
      username: username,
      email: `${username}@example.com`,
      name: username,
      sub: `mock-${username}-123`
    };
    setUser(mockUser);
    return { isSignInComplete: true };
  };

  const mockSignUp = async (username, password, email, name) => {
    // In dev mode, immediately complete signup
    const mockUser = {
      username: username,
      email: email,
      name: name,
      sub: `mock-${username}-123`
    };
    setUser(mockUser);
    return { isSignUpComplete: true };
  };

  const mockSignOut = async () => {
    setUser(null);
  };

  const mockConfirmSignUp = async (username, code) => {
    return { isSignUpComplete: true };
  };

  const mockResendConfirmationCode = async (username) => {
    return { success: true };
  };

  // Real AWS functions
  const getAccessToken = async () => {
    if (isDevMode) return mockGetAccessToken();
    try {
      const session = await fetchAuthSession();
      return session.tokens?.accessToken?.toString() || null;
    } catch (error) {
      console.error('Error getting access token:', error);
      return null;
    }
  };

  const checkAuthState = async () => {
    if (isDevMode) return mockCheckAuthState();
    try {
      const currentUser = await getCurrentUser();
      if (currentUser) {
        setUser({
          username: currentUser.username,
          email: currentUser.signInDetails?.loginId || '',
          name: currentUser.signInDetails?.loginId || currentUser.username,
          sub: currentUser.userId || ''
        });
      } else {
        setUser(null);
      }
    } catch (error) {
      console.log('No authenticated user found');
      setUser(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    checkAuthState();
  }, []);

  const handleSignIn = async (username, password) => {
    if (isDevMode) return mockSignIn(username, password);
    try {
      const result = await signIn({ username, password });
      await checkAuthState(); // Refresh user state
      return result;
    } catch (error) {
      console.error('Sign in error:', error);
      throw error;
    }
  };

  const handleSignUp = async (username, password, email, name) => {
    if (isDevMode) return mockSignUp(username, password, email, name);
    try {
      const result = await signUp({
        username,
        password,
        options: {
          userAttributes: {
            email,
            name
          }
        }
      });
      return result;
    } catch (error) {
      console.error('Sign up error:', error);
      throw error;
    }
  };

  const handleSignOut = async () => {
    if (isDevMode) return mockSignOut();
    try {
      await signOut();
      setUser(null);
    } catch (error) {
      console.error('Sign out error:', error);
      throw error;
    }
  };

  const handleConfirmSignUp = async (username, code) => {
    if (isDevMode) return mockConfirmSignUp(username, code);
    try {
      const result = await confirmSignUp({ username, confirmationCode: code });
      return result;
    } catch (error) {
      console.error('Confirm sign up error:', error);
      throw error;
    }
  };

  const handleResendConfirmationCode = async (username) => {
    if (isDevMode) return mockResendConfirmationCode(username);
    try {
      const result = await resendSignUpCode({ username });
      return result;
    } catch (error) {
      console.error('Resend confirmation code error:', error);
      throw error;
    }
  };

  const value = {
    user,
    loading,
    signIn: handleSignIn,
    signUp: handleSignUp,
    signOut: handleSignOut,
    confirmSignUp: handleConfirmSignUp,
    resendConfirmationCode: handleResendConfirmationCode,
    getAccessToken
  };

  return React.createElement(AuthContext.Provider, { value }, children);
};
