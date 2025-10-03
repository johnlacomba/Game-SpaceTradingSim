import React, { createContext, useContext, useEffect, useState } from 'react';
import { Amplify } from 'aws-amplify';
import {
  fetchAuthSession,
  fetchUserAttributes,
  getCurrentUser,
  signInWithRedirect,
  signOut as amplifySignOut
} from 'aws-amplify/auth';
import awsConfig from '../aws-config.js';

// Check if we're in development mode without AWS configuration
const isDevMode = import.meta.env.DEV && (!awsConfig.userPoolId || !awsConfig.userPoolWebClientId);

// Configure Amplify only if we have valid AWS configuration
if (!isDevMode) {
  try {
    Amplify.configure({
      Auth: {
        Cognito: {
          region: awsConfig.region,
          userPoolId: awsConfig.userPoolId,
          userPoolClientId: awsConfig.userPoolWebClientId,
          identityPoolId: awsConfig.identityPoolId,
          loginWith: {
            oauth: awsConfig.oauth
          }
        }
      }
    });
  } catch (error) {
    console.error('Error configuring Amplify:', error);
  }
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

  useEffect(() => {
    const isCallback = typeof window !== 'undefined' && window.location.pathname.startsWith('/auth/callback');
    if (!isDevMode && isCallback) {
      const complete = async () => {
        try {
          await fetchAuthSession();
          try {
            const currentUser = await getCurrentUser();
            const attrs = await fetchUserAttributes();
            setUser({
              username: currentUser.username,
              email: attrs.email || '',
              name: attrs.name || attrs.email || currentUser.username,
              sub: attrs.sub || ''
            });
          } catch (inner) {
            console.warn('Unable to populate user immediately after callback', inner);
          }
        } catch (error) {
          console.error('Hosted UI callback processing failed', error);
        } finally {
          const target = window.location.origin + '/';
          window.history.replaceState({}, document.title, target);
        }
      };
      complete();
    }
  }, []);

  const mockUser = {
    username: 'devuser',
    email: 'dev@example.com',
    name: 'Development User',
    sub: 'dev-user-123'
  };

  const mockGetAccessToken = async () => 'mock-jwt-token';

  const mockSignIn = async () => {
    setUser(mockUser);
    setLoading(false);
  };

  const mockSignOut = async () => {
    setUser(null);
  };

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
    if (isDevMode) {
      await mockSignIn();
      return;
    }

    try {
      const currentUser = await getCurrentUser();
      if (currentUser) {
        const attrs = await fetchUserAttributes();
        setUser({
          username: currentUser.username,
          email: attrs.email || '',
          name: attrs.name || attrs.email || currentUser.username,
          sub: attrs.sub || ''
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
    const doCheck = async () => {
      try {
        await fetchAuthSession();
      } catch (e) {
        // ignore - likely no session yet
      }
      await checkAuthState();
    };
    doCheck();
  }, []);

  const signInWithHostedUI = async () => {
    if (isDevMode) return mockSignIn();
    try {
      await signInWithRedirect();
    } catch (error) {
      console.error('Hosted UI redirect error:', error);
      throw error;
    }
  };

  const handleSignOut = async () => {
    if (isDevMode) return mockSignOut();
    try {
      await amplifySignOut();
      setUser(null);
    } catch (error) {
      console.error('Sign out error:', error);
      throw error;
    }
  };

  const value = {
    user,
    loading,
    signInWithHostedUI,
    signOut: handleSignOut,
    getAccessToken
  };

  return React.createElement(AuthContext.Provider, { value }, children);
};
