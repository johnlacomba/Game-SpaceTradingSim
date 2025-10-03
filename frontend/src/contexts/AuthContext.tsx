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

// Check if we're in development mode
const isDevMode = process.env.NODE_ENV !== 'production' && import.meta.env.VITE_USE_AWS_AUTH !== 'true';

//

// Configure Amplify only in production mode or when explicitly enabled
if (!isDevMode) {
  try {
    Amplify.configure({
      Auth: {
        Cognito: {
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

interface User {
  username: string;
  email: string;
  name: string;
  sub: string;
}

interface AuthContextType {
  user: User | null;
  loading: boolean;
  signInWithHostedUI: () => Promise<void>;
  signOut: () => Promise<void>;
  getAccessToken: () => Promise<string | null>;
}

const AuthContext = createContext<AuthContextType>({} as AuthContextType);

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};

interface AuthProviderProps {
  children: React.ReactNode;
}

export const AuthProvider: React.FC<AuthProviderProps> = ({ children }) => {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  // Ensure Hosted UI callback is processed and URL cleaned
  useEffect(() => {
    const isCallback = window.location.pathname.startsWith('/auth/callback')
    if (!isDevMode && isCallback) {
      // Complete the Hosted UI redirect flow (exchanges code for tokens)
      const complete = async () => {
        try {
          // Callback detected
          // Exchange code for tokens
          await fetchAuthSession()
          // Session fetched after callback
          // Attempt to populate user immediately
          try {
            const currentUser = await getCurrentUser()
            const attrs = await fetchUserAttributes()
            setUser({
              username: currentUser.username,
              email: attrs.email || '',
              name: attrs.name || attrs.email || currentUser.username,
              sub: attrs.sub || ''
            })
            // User set after callback
          } catch (e) {
            // getCurrentUser after callback failed
          }
        } catch (e) {
          // Callback processing error
        } finally {
          // Clean the URL
          const target = window.location.origin + '/'
          window.history.replaceState({}, document.title, target)
        }
      }
      void complete()
    }
  }, [])

  // Mock functions for development mode
  const mockGetAccessToken = async (): Promise<string> => {
    return 'mock-jwt-token';
  };

  const mockCheckAuthState = async () => {
    // In dev mode, automatically sign in with a mock user
    const mockUser: User = {
      username: 'devuser',
      email: 'dev@example.com',
      name: 'Development User',
      sub: 'dev-user-123'
    };
    setUser(mockUser);
    setLoading(false);
  };

  const mockHostedUISignIn = async () => {
    const mockUser: User = {
      username: 'devuser',
      email: 'dev@example.com',
      name: 'Development User',
      sub: 'dev-user-123'
    };
    setUser(mockUser);
    setLoading(false);
  };

  const mockSignOut = async () => {
    setUser(null);
  };

  const getAccessToken = async (): Promise<string | null> => {
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
  // Check auth state
    if (isDevMode) return mockCheckAuthState();
    
    try {
      // Add timeout to prevent hanging
      const timeoutPromise = new Promise((_, reject) => 
        setTimeout(() => reject(new Error('Auth check timeout')), 5000)
      );
      
      const authPromise = getCurrentUser();
      const currentUser = await Promise.race([authPromise, timeoutPromise]) as any;
      
  // Current user fetched
      if (currentUser) {
        const userAttributes = await fetchUserAttributes();
  // User attributes fetched
        setUser({
          username: currentUser.username,
          email: userAttributes.email || '',
          name: userAttributes.name || userAttributes.email || currentUser.username,
          sub: userAttributes.sub || ''
        });
      } else {
        setUser(null);
      }
    } catch (error) {
  // No authenticated user
      setUser(null);
    } finally {
  // Loading complete
      setLoading(false);
    }
  };

  useEffect(() => {
    // First process any hosted UI callback, then check auth
    const doCheck = async () => {
      try {
        await fetchAuthSession()
      } catch {}
      await checkAuthState()
    }
    doCheck()
  }, []);

  const signInWithHostedUI = async () => {
    if (isDevMode) return mockHostedUISignIn();
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

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};
