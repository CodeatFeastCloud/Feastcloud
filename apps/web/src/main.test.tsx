import { afterEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  createRoot: vi.fn(),
  loadConfiguredLanguagePacks: vi.fn(() => new Promise<void>(() => undefined)),
  registerServiceWorker: vi.fn(),
  render: vi.fn(),
}));

vi.mock('react-dom/client', () => ({
  default: {
    createRoot: mocks.createRoot,
  },
}));
vi.mock('./App', () => ({ default: () => null }));
vi.mock('./i18n', () => ({
  loadConfiguredLanguagePacks: mocks.loadConfiguredLanguagePacks,
}));
vi.mock('./registerServiceWorker', () => ({
  registerServiceWorker: mocks.registerServiceWorker,
}));

describe('application bootstrap', () => {
  afterEach(() => {
    document.body.replaceChildren();
  });

  it('renders before an optional language network request settles', async () => {
    const rootElement = document.createElement('div');
    rootElement.id = 'root';
    document.body.append(rootElement);
    mocks.createRoot.mockReturnValue({ render: mocks.render });

    await import('./main');

    expect(mocks.render).toHaveBeenCalledOnce();
    expect(mocks.loadConfiguredLanguagePacks).toHaveBeenCalledOnce();
    expect(mocks.render.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.loadConfiguredLanguagePacks.mock.invocationCallOrder[0],
    );
    expect(mocks.registerServiceWorker).toHaveBeenCalledOnce();
  });
});
