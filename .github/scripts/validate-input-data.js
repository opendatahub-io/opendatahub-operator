/**
 * Validate and prepare input data from workflow_dispatch or repository_dispatch
 * Handles component data from either trigger type and writes to file for subsequent steps
 */

module.exports = async ({ core, context }) => {
    try {
        // Get component data from either workflow_dispatch input or repository_dispatch payload
        let componentData;

        if (context.eventName === 'repository_dispatch') {
            componentData = context.payload.client_payload.component_data;
            console.log('Using component data from repository_dispatch');
        } else if (context.eventName === 'workflow_dispatch') {
            componentData = context.payload.inputs.component_data;
            console.log('Using component data from workflow_dispatch');
        } else {
            core.setFailed(`Unsupported event type: ${context.eventName}`);
            return;
        }

        if (!componentData) {
            core.setFailed('Component data is required');
            return;
        }

        // Validate JSON structure
        let parsed;
        try {
            parsed = typeof componentData === 'string' ? JSON.parse(componentData) : componentData;
        } catch (error) {
            core.setFailed(`Invalid JSON in component data: ${error.message}`);
            return;
        }

        // Validate that it's an object with expected structure
        if (typeof parsed !== 'object' || parsed === null) {
            core.setFailed('Component data must be a JSON object');
            return;
        }

        console.log('Component data validated successfully');
        console.log('Component data preview:', JSON.stringify(parsed, null, 2));

        // Set outputs for subsequent steps (no file I/O needed)
        core.setOutput('component_data', JSON.stringify(parsed));
        core.setOutput('component_count', Object.keys(parsed).length);

        console.log(`Component data set as step output (${Object.keys(parsed).length} components)`);

    } catch (error) {
        console.error('Error validating input data:', error);
        core.setFailed(`Input data validation failed: ${error.message}`);
    }
};
