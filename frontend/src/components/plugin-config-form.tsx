import { useState, useEffect } from 'react';
import { GetPluginConfig, SetPluginConfig } from '../../wailsjs/go/main/App';

// Type definitions for JSON Schema
type JSONSchemaProperty = {
  type?: string;
  enum?: any[];
  default?: any;
  description?: string;
  title?: string;
  minimum?: number;
  maximum?: number;
  [key: string]: any;
};

type JSONSchema = {
  type?: string;
  properties?: Record<string, JSONSchemaProperty>;
  required?: string[];
  [key: string]: any;
};

interface PluginConfigFormProps {
  pluginName: string;
  schema?: JSONSchema;
  onSave?: () => void;
}

export function PluginConfigForm({ pluginName, schema, onSave }: PluginConfigFormProps) {
  const [config, setConfig] = useState<Record<string, any>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadConfig();
  }, [pluginName]);

  const loadConfig = async () => {
    try {
      setLoading(true);
      const currentConfig = await GetPluginConfig(pluginName);
      setConfig(currentConfig || {});
      setError(null);
    } catch (err) {
      setError(`Failed to load config: ${err}`);
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async () => {
    try {
      setSaving(true);
      setError(null);
      await SetPluginConfig(pluginName, config);
      if (onSave) {
        onSave();
      }
    } catch (err) {
      setError(`Failed to save config: ${err}`);
    } finally {
      setSaving(false);
    }
  };

  const updateField = (key: string, value: any) => {
    setConfig((prev) => ({ ...prev, [key]: value }));
  };

  const getDefaultValue = (prop: JSONSchemaProperty): any => {
    if (prop.default !== undefined) {
      return prop.default;
    }
    switch (prop.type) {
      case 'string':
        return '';
      case 'number':
        return 0;
      case 'boolean':
        return false;
      case 'array':
        return [];
      case 'object':
        return {};
      default:
        return null;
    }
  };

  const renderField = (key: string, prop: JSONSchemaProperty) => {
    const value = config[key] !== undefined ? config[key] : getDefaultValue(prop);
    const title = prop.title || key;
    const description = prop.description;

    switch (prop.type) {
      case 'string':
        if (prop.enum && Array.isArray(prop.enum)) {
          return (
            <div key={key} className="mb-4">
              <label className="block text-sm font-medium mb-1">{title}</label>
              {description && <p className="text-xs text-muted-foreground mb-2">{description}</p>}
              <select
                value={value}
                onChange={(e) => updateField(key, e.target.value)}
                className="w-full px-3 py-2 border border-border rounded focus:outline-none focus:ring-2 focus:ring-primary"
              >
                {prop.enum.map((option: any) => (
                  <option key={option} value={option}>
                    {option}
                  </option>
                ))}
              </select>
            </div>
          );
        }
        return (
          <div key={key} className="mb-4">
            <label className="block text-sm font-medium mb-1">{title}</label>
            {description && <p className="text-xs text-muted-foreground mb-2">{description}</p>}
            <input
              type="text"
              value={value}
              onChange={(e) => updateField(key, e.target.value)}
              className="w-full px-3 py-2 border border-border rounded focus:outline-none focus:ring-2 focus:ring-primary"
            />
          </div>
        );

      case 'number':
        return (
          <div key={key} className="mb-4">
            <label className="block text-sm font-medium mb-1">{title}</label>
            {description && <p className="text-xs text-muted-foreground mb-2">{description}</p>}
            <input
              type="number"
              value={value}
              onChange={(e) => updateField(key, Number(e.target.value))}
              min={prop.minimum}
              max={prop.maximum}
              className="w-full px-3 py-2 border border-border rounded focus:outline-none focus:ring-2 focus:ring-primary"
            />
          </div>
        );

      case 'boolean':
        return (
          <div key={key} className="mb-4">
            <label className="flex items-center">
              <input
                type="checkbox"
                checked={value}
                onChange={(e) => updateField(key, e.target.checked)}
                className="mr-2"
              />
              <span className="text-sm font-medium">{title}</span>
            </label>
            {description && <p className="text-xs text-muted-foreground mt-1 ml-6">{description}</p>}
          </div>
        );

      default:
        return (
          <div key={key} className="mb-4">
            <label className="block text-sm font-medium mb-1">{title}</label>
            {description && <p className="text-xs text-muted-foreground mb-2">{description}</p>}
            <input
              type="text"
              value={JSON.stringify(value)}
              onChange={(e) => {
                try {
                  updateField(key, JSON.parse(e.target.value));
                } catch {
                  // Invalid JSON, ignore
                }
              }}
              className="w-full px-3 py-2 border border-border rounded focus:outline-none focus:ring-2 focus:ring-primary"
            />
          </div>
        );
    }
  };

  if (loading) {
    return <div className="p-4 text-muted-foreground">Loading configuration...</div>;
  }

  if (!schema || !schema.properties) {
    return <div className="p-4 text-muted-foreground">No configuration schema available for this plugin.</div>;
  }

  return (
    <div className="p-4">
      {error && (
        <div className="mb-4 p-3 bg-red-500/20 text-red-500 border border-red-500/30 rounded">
          {error}
        </div>
      )}
      <div className="space-y-4">
        {Object.entries(schema.properties).map(([key, prop]) => renderField(key, prop))}
      </div>
      <div className="mt-6 flex justify-end">
        <button
          onClick={handleSave}
          disabled={saving}
          className="px-4 py-2 bg-primary text-primary-foreground rounded hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {saving ? 'Saving...' : 'Save Configuration'}
        </button>
      </div>
    </div>
  );
}
