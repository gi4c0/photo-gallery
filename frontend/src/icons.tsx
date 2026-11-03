// Stroke icons from the design. All are decorative: label the control, not the icon.
import type { ReactNode } from 'react';

function Icon({ d, size = 18, width = 1.7, fill = 'none', children }: {
  d?: string; size?: number; width?: number; fill?: string; children?: ReactNode;
}) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill={fill} stroke="currentColor" strokeWidth={width}
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {d && <path d={d} />}
      {children}
    </svg>
  );
}

export const PlusIcon = () => <Icon d="M12 5v14M5 12h14" width={1.8} />;
export const BackIcon = () => <Icon d="M15 6l-6 6 6 6" width={1.8} />;
export const NextIcon = () => <Icon d="M9 6l6 6-6 6" size={20} />;
export const PrevIcon = () => <Icon d="M15 6l-6 6 6 6" size={20} />;
export const LeftIcon = () => <Icon d="M15 6l-6 6 6 6" size={16} />;
export const RightIcon = () => <Icon d="M9 6l6 6-6 6" size={16} />;
export const TrashIcon = () => <Icon d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" size={16} width={1.8} />;
export const UploadIcon = () => <Icon d="M12 16V4M7 9l5-5 5 5M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3" size={26} width={1.5} />;
export const DownloadIcon = ({ size = 18 }: { size?: number }) => <Icon d="M12 4v12M7 11l5 5 5-5M4 20h16" size={size} />;
export const CloseIcon = () => <Icon d="M6 6l12 12M18 6L6 18" size={22} />;
export const LinkIcon = () => (
  <Icon d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1" size={16} width={1.8} />
);
export const EyeIcon = () => (
  <Icon d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z" width={1.6}>
    <circle cx="12" cy="12" r="3" />
  </Icon>
);
export const StarIcon = ({ filled }: { filled: boolean }) => (
  <Icon d="M12 3.5l2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.9l-5.2 2.7 1-5.8L3.5 9.7l5.9-.9z" width={1.6}
    fill={filled ? 'currentColor' : 'none'} />
);
export const EyeOffIcon = () => (
  <Icon d="M3 3l18 18M10.6 5.1A10.7 10.7 0 0 1 12 5c6.4 0 10 7 10 7a17.6 17.6 0 0 1-3.2 4.1M6.6 6.6C3.8 8.4 2 12 2 12s3.6 7 10 7a9.9 9.9 0 0 0 5.4-1.6M9.9 9.9a3 3 0 0 0 4.2 4.2" width={1.6} />
);
