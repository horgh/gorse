"use strict";

if (Gorse === undefined) {
	var Gorse = {};
}

//! write a message to the console
/*!
 * @param string msg
 *
 * @return void
 */
Gorse.log = function(msg) {
	if (!window || !window.console || !window.console.log ||
			msg === undefined) {
		return;
	}
	window.console.log(msg);
};

// Toggle between 3 states:
// - none, read, archive
Gorse.toggle_read_state = function(item_li) {
	// Read -> archive.
	if ($(item_li).hasClass('read')) {
		$(item_li).removeClass('read');
		$(item_li).addClass('archive');

		$('input.state-change', item_li).attr('name', 'archive-item');
		return;
	}

	// Archive -> none.
	if ($(item_li).hasClass('archive')) {
		$(item_li).removeClass('archive');

		$('input.state-change', item_li).remove();
		return;
	}

	// None -> read
	$(item_li).addClass('read');

	var read_ele = $('<input>').attr({
		'type':  'hidden',
		'name':  'read-item',
		'class': 'state-change',
		'value': $('.item_id', item_li).val()
	});

	$(item_li).append(read_ele);
};

// Determine how many items we have selected. Update a displayed counter.
Gorse.update_counts = function() {
	// Count how many we have selected.
	var count = $('input.state-change').length;

	// Display the counter in the save button.
	var label = 'Save (' + count + ')';
	$('button#update-flags-top').text(label);
};

$(document).ready(function() {
	// Add a click handler to all feed item rows.
	$('ul#items > li').each(function(i, li) {
		$(li).click(function() {
			Gorse.toggle_read_state($(this));
			Gorse.update_counts();
		});
	});

	// Hide the list of feed information by default.
	$('ul#feeds').hide();

	// Setup the 'feed expand' link.
	$('a.expand_feeds')
		.text('+ Expand')
		.click(function(evt) {
			evt.preventDefault();

			if ($(this).text() === '+ Expand') {
				$(this).text('- Collapse');
				$('#feeds').show(400);
			} else {
				$(this).text('+ Expand');
				$('#feeds').hide(400);
			}
		});

	// When we click the save button, submit the form with our read elements.
	$('button#update-flags-top').click(function() {
		$('form#list-items-form').submit();
	});
});
