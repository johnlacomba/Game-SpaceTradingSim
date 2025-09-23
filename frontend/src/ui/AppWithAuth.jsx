import React from 'react';
import { AuthProvider } from '../contexts/AuthContext.jsx';
import { App } from './App';

export function AppWithAuth() {
  return React.createElement(
    AuthProvider,
    null,
    React.createElement(App)
  );
}

export default AppWithAuth;
