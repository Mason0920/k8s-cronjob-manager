import { createContext, useContext } from 'react';
import type { AppConfig } from './types';

export const ConfigContext = createContext<AppConfig>({ namespaces: [], readOnly: true, labelSelector: '' });

export function useAppConfig(): AppConfig {
  return useContext(ConfigContext);
}
