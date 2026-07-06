export interface PortMapping {
  ip?: string;
  private_port: number;
  public_port?: number;
  type: string;
}

export interface Container {
  fullid: string;
  id: string;
  name: string;
  status: string;
  cpu: string;
  memory: string;
  memorypercent?: number;
  memorylimit?: string;
  blkio: string;
  network: string;
  healthscore?: number;
  healthstatus?: 'healthy' | 'warning' | 'dangerous';
  ports?: PortMapping[];
}

export interface ToastMessage {
  id: number;
  message: string;
  type: 'info' | 'success' | 'error';
}
