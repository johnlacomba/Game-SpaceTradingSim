import React from 'react';
import { AuthProvider } from '../contexts/AuthContext';
import { App } from './App';

export function AppWithAuth() {
  return (
    <AuthProvider>
      <App />
    </AuthProvider>
  );
}

export default AppWithAuth;
