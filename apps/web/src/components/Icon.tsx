import type { ReactNode, SVGProps } from 'react';

export type IconName =
  | 'brand'
  | 'overview'
  | 'plus'
  | 'kitchen'
  | 'search'
  | 'wifi'
  | 'offline'
  | 'clock'
  | 'bag'
  | 'table'
  | 'truck'
  | 'arrow'
  | 'minus'
  | 'check'
  | 'flame'
  | 'sparkles'
  | 'bell'
  | 'volume'
  | 'pause';

const paths: Record<IconName, ReactNode> = {
  brand: <path d="M7 3v8a5 5 0 0 0 10 0V3M9 3v8a3 3 0 0 0 6 0V3M5 21c2.2-1 4.5-1.5 7-1.5s4.8.5 7 1.5" />,
  overview: <path d="M4 13h6V4H4v9Zm0 7h6v-4H4v4Zm10 0h6v-9h-6v9Zm0-12h6V4h-6v4Z" />,
  plus: <path d="M12 5v14M5 12h14" />,
  kitchen: <path d="M4 4v5a3 3 0 0 0 3 3h1V4M6 4v5M8 4v5m5-5v16m0-10c4 0 6-2 6-6" />,
  search: <path d="m21 21-4.3-4.3m2.3-5.2a7.5 7.5 0 1 1-15 0 7.5 7.5 0 0 1 15 0Z" />,
  wifi: <path d="M5 12.6a10 10 0 0 1 14 0M8.5 16a5 5 0 0 1 7 0M12 20h.01M2 9a14.2 14.2 0 0 1 20 0" />,
  offline: <path d="m3 3 18 18M8.5 16a5 5 0 0 1 7 0M12 20h.01M5 12.6a10 10 0 0 1 6-2.6M15 10.5a10 10 0 0 1 4 2.1M2 9a14 14 0 0 1 3.3-2.4M9 5.2A14 14 0 0 1 22 9" />,
  clock: <path d="M12 7v5l3 2m6-2a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />,
  bag: <path d="M6 8h12l1 13H5L6 8Zm3 0V6a3 3 0 0 1 6 0v2" />,
  table: <path d="M4 9h16M6 9v11m12-11v11M3 5h18v4H3V5Z" />,
  truck: <path d="M3 5h11v11H3V5Zm11 5h4l3 3v3h-7v-6ZM7 19a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm11 0a2 2 0 1 0 0-4 2 2 0 0 0 0 4Z" />,
  arrow: <path d="M5 12h14m-5-5 5 5-5 5" />,
  minus: <path d="M5 12h14" />,
  check: <path d="m5 12 4 4L19 6" />,
  flame: <path d="M13 3s1 4-2 6c-2-2-4-1-5 1-2 4 1 10 6 10s8-4 7-8c-.5-2-2-4-4-5 0 3-2 4-2 4s1-5 0-8Z" />,
  sparkles: <path d="m12 3 1.2 3.8L17 8l-3.8 1.2L12 13l-1.2-3.8L7 8l3.8-1.2L12 3Zm6 10 .8 2.2L21 16l-2.2.8L18 19l-.8-2.2L15 16l2.2-.8L18 13ZM5 14l.8 2.2L8 17l-2.2.8L5 20l-.8-2.2L2 17l2.2-.8L5 14Z" />,
  bell: <path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4" />,
  volume: <path d="M5 10v4h3l4 4V6L8 10H5Zm11.5-2.5a5 5 0 0 1 0 9M19 5a8.5 8.5 0 0 1 0 14" />,
  pause: <path d="M8 5v14M16 5v14" />,
};

export function Icon({ name, ...props }: { name: IconName } & SVGProps<SVGSVGElement>) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      focusable="false"
      {...props}
    >
      {paths[name]}
    </svg>
  );
}
